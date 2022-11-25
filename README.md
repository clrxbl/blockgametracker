# blockgametracker

A Python Prometheus exporter to query Minecraft servers for their current status and playercount.
Used for https://blockgametracker.iptables.sh

Originally https://github.com/itzg/mc-monitor was used for this purpose but it ended up not suiting this project. Metric & label names were taken from this project.

# Usage

See the production config as example on how to configure the server list in `deploy/config/servers.yaml`
The exporter can be ran using Docker. When developing locally, you can use `docker-compose up --build` to run the exporter and access it at `http://127.0.0.1:8080`