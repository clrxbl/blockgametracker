package ping

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/eko/gocache/lib/v4/cache"
	"github.com/eko/gocache/lib/v4/store"
	gocachestore "github.com/eko/gocache/store/go_cache/v4"
	"github.com/jamesog/iptoasn"
	"github.com/mcstatus-io/mcutil/v4/status"
	gocache "github.com/patrickmn/go-cache"

	"mcstatus-exporter/internal/config"
)

const (
	EditionJava    = "java"
	EditionBedrock = "bedrock"
)

type Result struct {
	Edition      string
	Name         string
	QueryAddress string
	ASNum        uint32
	ASName       string
	PlayerCount  *int64
	Time         time.Time
}

var (
	asnLookupCacheClient = gocache.New(1*time.Hour, 10*time.Minute)
	asnLookupCacheStore  = gocachestore.NewGoCache(asnLookupCacheClient)
	asnLookupCache       = cache.New[iptoasn.IP](asnLookupCacheStore)
)

func queryOne(edition, name, queryHostname string) *Result {
	executionTimer := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	resolvedHostname := queryHostname
	var playercount *int64

	switch edition {
	case EditionJava:
		response, err := status.Modern(ctx, queryHostname, 25565)
		if err != nil {
			log.Error("failed to get response: "+err.Error(), "edition", edition, "hostname", queryHostname)
			return nil
		}
		playercount = response.Players.Online
		if response.SRVRecord != nil {
			resolvedHostname = response.SRVRecord.Host
		}
	case EditionBedrock:
		response, err := status.Bedrock(ctx, queryHostname, 19132)
		if err != nil {
			log.Error("failed to get response: "+err.Error(), "edition", edition, "hostname", queryHostname)
			return nil
		}
		playercount = response.OnlinePlayers
	default:
		log.Error(fmt.Errorf("unknown edition: %s", edition))
		panic("unknown edition")
	}

	resolvedIP, err := net.LookupIP(resolvedHostname)
	if err != nil {
		log.Error(err.Error(), "hostname", resolvedHostname)
		return nil
	}

	ip, err := asnLookupCache.Get(ctx, resolvedIP[0])
	if errors.Is(err, store.NotFound{}) {
		log.Info("performing uncached asn lookup", "ip", resolvedIP[0])
		ip, err = iptoasn.LookupIP(fmt.Sprint(resolvedIP[0]))
		if err != nil {
			log.Error("unable to resolve asn: "+err.Error(), "hostname", resolvedHostname)
			ip = iptoasn.IP{ASName: "N/A", ASNum: 0, IP: resolvedHostname}
		} else {
			if err := asnLookupCache.Set(ctx, resolvedIP[0], ip); err != nil {
				panic(err)
			}
		}
	}

	log.Debug("resolved", "hostname", resolvedHostname, "ip", ip.IP, "asn", ip.ASNum)

	pc := "N/A"
	if playercount != nil {
		pc = strconv.FormatInt(*playercount, 10)
	}
	log.Info("finished querying server ", "edition", edition, "name", name, "players", pc, "execTimeMs", time.Since(executionTimer).Milliseconds())

	return &Result{
		Edition:      edition,
		Name:         name,
		QueryAddress: queryHostname,
		ASNum:        ip.ASNum,
		ASName:       ip.ASName,
		PlayerCount:  playercount,
		Time:         time.Now(),
	}
}

// QueryAll pings every enabled server across both editions concurrently and
// returns all successful results. Failed pings are logged and omitted.
func QueryAll(cfg config.Config) []Result {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []Result
	)

	dispatch := func(servers []config.Server, edition string) {
		for _, server := range servers {
			if server.Disabled {
				continue
			}
			wg.Add(1)
			go func(s config.Server) {
				defer wg.Done()
				r := queryOne(edition, s.Name, s.Address)
				if r == nil {
					return
				}
				mu.Lock()
				results = append(results, *r)
				mu.Unlock()
			}(server)
		}
	}

	dispatch(cfg.Java, EditionJava)
	dispatch(cfg.Bedrock, EditionBedrock)
	wg.Wait()

	return results
}
