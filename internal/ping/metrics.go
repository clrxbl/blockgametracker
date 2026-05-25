package ping

import (
	"strconv"

	"github.com/gosimple/slug"
	"github.com/prometheus/client_golang/prometheus"
)

var Gauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "minecraft_status_players_online_count",
	Help: "Minecraft server online player count",
}, []string{"server_edition", "server_name", "server_slug", "server_host", "as_number", "as_name"})

// ApplyToGauge resets the gauge and writes one sample per result, matching
// the labels emitted by the original scrape-triggered exporter.
func ApplyToGauge(results []Result) {
	Gauge.Reset()
	for _, r := range results {
		if r.PlayerCount == nil {
			continue
		}
		Gauge.WithLabelValues(
			r.Edition,
			r.Name,
			slug.Make(r.Name),
			r.QueryAddress,
			strconv.Itoa(int(r.ASNum)),
			r.ASName,
		).Set(float64(*r.PlayerCount))
	}
}
