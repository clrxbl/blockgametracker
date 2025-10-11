package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/eko/gocache/lib/v4/cache"
	"github.com/eko/gocache/lib/v4/store"
	gocachestore "github.com/eko/gocache/store/go_cache/v4"
	"github.com/gosimple/slug"
	"github.com/jamesog/iptoasn"
	"github.com/mcstatus-io/mcutil/v4/status"
	gocache "github.com/patrickmn/go-cache"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"
)

type Server struct {
	Name     string `yaml:"name"`
	Address  string `yaml:"address"`
	Disabled bool   `yaml:"disabled"`
}

type Config struct {
	Java    []Server `yaml:"java"`
	Bedrock []Server `yaml:"bedrock"`
}

var config Config

var asnLookupCacheClient = gocache.New(1*time.Hour, 10*time.Minute)
var asnLookupCacheStore = gocachestore.NewGoCache(asnLookupCacheClient)
var asnLookupCache = cache.New[iptoasn.IP](asnLookupCacheStore)

var promGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "minecraft_status_players_online_count",
	Help: "Minecraft server online player count",
}, []string{"server_edition", "server_name", "server_slug", "server_host", "as_number", "as_name"})

func getEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		value = fallback
	}
	return value
}

func index(w http.ResponseWriter) {
	_, err := fmt.Fprintf(w, "mcstatus-exporter")
	if err != nil {
		return
	}
}

func query(edition string, name string, queryHostname string) {
	executionTimer := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	var resolvedHostname string = queryHostname
	var playercount *int64

	switch edition {
	case "java":
		response, err := status.Modern(ctx, queryHostname, 25565)
		if err != nil {
			log.Error("failed to get response: "+err.Error(), "edition", edition, "hostname", queryHostname)
			return
		}
		playercount = response.Players.Online
		if response.SRVRecord != nil {
			resolvedHostname = response.SRVRecord.Host
		}
	case "bedrock":
		response, err := status.Bedrock(ctx, queryHostname, 19132)
		if err != nil {
			log.Error("failed to get response: "+err.Error(), "edition", edition, "hostname", queryHostname)
			return
		}
		playercount = response.OnlinePlayers
		// Bedrock doesn't have SRV records so no need to handle those
	default:
		log.Error(fmt.Errorf("unknown edition: %s", edition))
		panic("unknown edition")
	}

	resolvedIP, err := net.LookupIP(resolvedHostname)
	if err != nil {
		log.Error(err.Error(), "hostname", resolvedHostname)
		return
	}

	ip, err := asnLookupCache.Get(ctx, resolvedIP[0])
	if errors.Is(err, store.NotFound{}) {
		log.Info("performing uncached asn lookup", "ip", resolvedIP[0])
		ip, err = iptoasn.LookupIP(fmt.Sprint(resolvedIP[0]))
		// TODO: probably want to continue with fallback N/A AS values if this fails
		if err != nil {
			panic(err)
		}

		err = asnLookupCache.Set(ctx, resolvedIP[0], ip)
		if err != nil {
			panic(err)
		}
	}

	log.Debug("resolved", "hostname", resolvedHostname, "ip", ip.IP, "asn", ip.ASNum)

	log.Info("finished querying server ", "edition", edition, "name", name, "players", strconv.FormatInt(*playercount, 10), "execTimeMs", time.Since(executionTimer).Milliseconds())

	promGauge.WithLabelValues(edition, name, slug.Make(name), queryHostname, strconv.Itoa(int(ip.ASNum)), ip.ASName).Set(float64(*playercount))
}

func queryServers(servers []Server, serverType string, wg *sync.WaitGroup) {
	for _, server := range servers {
		if !server.Disabled {
			wg.Add(1)
			go func(server Server) {
				defer wg.Done()
				query(serverType, server.Name, server.Address)
			}(server)
		}
	}
}

func promMetrics(w http.ResponseWriter, r *http.Request) {
	var wg sync.WaitGroup

	queryServers(config.Java, "java", &wg)
	queryServers(config.Bedrock, "bedrock", &wg)

	wg.Wait()

	promhttp.Handler().ServeHTTP(w, r)
}

func updateConfig() {
	file, err := os.Open(getEnv("CONFIG_FILE", "servers.yaml"))
	if err != nil {
		log.Fatalf("error opening YAML file: %v", err)
		panic(err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			panic(err)
		}
	}(file)

	decoder := yaml.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		log.Fatalf("error decoding YAML: %v", err)
		panic(err)
	}

	log.Info("loaded config", "java", len(config.Java), "bedrock", len(config.Bedrock))
}

func main() {
	log.Info("mcstatus-exporter")
	updateConfig()

	prometheus.MustRegister(promGauge)
	http.HandleFunc("/metrics", promMetrics)

	var httpBindAddr string = getEnv("BIND", ":8080")
	log.Infof("listening on %s", httpBindAddr)
	err := http.ListenAndServe(httpBindAddr, nil)
	if err != nil {
		log.Error(err, "error starting HTTP server")
		panic(err)
	}
}
