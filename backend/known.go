package main

import "strings"

// Known apps: friendly names, suggested nicknames, web ports and whether the
// container is a "helper" (database, cache, exporter) that should fold into
// the card of the app it serves.
type KnownApp struct {
	Name     string `json:"name"`               // proper product name
	Nickname string `json:"nickname,omitempty"` // suggested nickname (what it does)
	Port     int    `json:"port,omitempty"`     // container (or host-network) port of the web UI
	Scheme   string `json:"scheme,omitempty"`   // http (default) or https
	Helper   bool   `json:"helper,omitempty"`
	HelperOf string `json:"helperOf,omitempty"` // preferred parent app slug, e.g. exporters -> prometheus
	Icon     string `json:"-"`                  // dashboard-icons slug if it differs from the key
}

var knownApps = map[string]KnownApp{
	// media
	"radarr":                  {Name: "Radarr", Nickname: "Movies", Port: 7878},
	"sonarr":                  {Name: "Sonarr", Nickname: "TV Shows", Port: 8989},
	"lidarr":                  {Name: "Lidarr", Nickname: "Music", Port: 8686},
	"readarr":                 {Name: "Readarr", Nickname: "Books", Port: 8787},
	"prowlarr":                {Name: "Prowlarr", Nickname: "Indexers", Port: 9696},
	"bazarr":                  {Name: "Bazarr", Nickname: "Subtitles", Port: 6767},
	"overseerr":               {Name: "Overseerr", Nickname: "Requests", Port: 5055},
	"jellyseerr":              {Name: "Jellyseerr", Nickname: "Requests", Port: 5055},
	"sabnzbd":                 {Name: "SABnzbd", Nickname: "Downloads (Usenet)", Port: 8080},
	"nzbget":                  {Name: "NZBGet", Nickname: "Downloads (Usenet)", Port: 6789},
	"qbittorrent":             {Name: "qBittorrent", Nickname: "Downloads (Torrent)", Port: 8080},
	"transmission":            {Name: "Transmission", Nickname: "Downloads (Torrent)", Port: 9091},
	"jellyfin":                {Name: "Jellyfin", Nickname: "Media Server", Port: 8096},
	"plex":                    {Name: "Plex", Nickname: "Media Server", Port: 32400},
	"emby":                    {Name: "Emby", Nickname: "Media Server", Port: 8096},
	"tautulli":                {Name: "Tautulli", Nickname: "Plex Stats", Port: 8181},
	"navidrome":               {Name: "Navidrome", Nickname: "Music Streaming", Port: 4533},
	"audiobookshelf":          {Name: "Audiobookshelf", Nickname: "Audiobooks", Port: 80},
	"immich-server":           {Name: "Immich", Nickname: "Photos", Port: 2283, Icon: "immich"},
	"immich-machine-learning": {Name: "Immich ML", Helper: true, Icon: "immich"},
	"photoprism":              {Name: "PhotoPrism", Nickname: "Photos", Port: 2342},
	// cloud / productivity
	"nextcloud":      {Name: "Nextcloud", Nickname: "Cloud", Port: 80},
	"paperless-ngx":  {Name: "Paperless-ngx", Nickname: "Documents", Port: 8000},
	"vaultwarden":    {Name: "Vaultwarden", Nickname: "Passwords", Port: 80},
	"gitea":          {Name: "Gitea", Nickname: "Git", Port: 3000},
	"forgejo":        {Name: "Forgejo", Nickname: "Git", Port: 3000},
	"home-assistant": {Name: "Home Assistant", Nickname: "Smart Home", Port: 8123},
	"homeassistant":  {Name: "Home Assistant", Nickname: "Smart Home", Port: 8123, Icon: "home-assistant"},
	"syncthing":      {Name: "Syncthing", Nickname: "File Sync", Port: 8384},
	"freshrss":       {Name: "FreshRSS", Nickname: "News", Port: 80},
	"mealie":         {Name: "Mealie", Nickname: "Recipes", Port: 9000},
	// infra
	"nginx-proxy-manager": {Name: "Nginx Proxy Manager", Nickname: "Reverse Proxy", Port: 81},
	"traefik":             {Name: "Traefik", Nickname: "Reverse Proxy", Port: 8080},
	"caddy":               {Name: "Caddy", Nickname: "Web Server"},
	"nginx":               {Name: "nginx", Nickname: "Web Server", Port: 80},
	"portainer":           {Name: "Portainer", Nickname: "Docker Admin", Port: 9443, Scheme: "https"},
	"pihole":              {Name: "Pi-hole", Nickname: "Ad Blocker", Port: 80, Icon: "pi-hole"},
	"adguardhome":         {Name: "AdGuard Home", Nickname: "Ad Blocker", Port: 3000, Icon: "adguard-home"},
	"uptime-kuma":         {Name: "Uptime Kuma", Nickname: "Uptime", Port: 3001},
	"wireguard":           {Name: "WireGuard", Nickname: "VPN"},
	"wg-easy":             {Name: "WireGuard", Nickname: "VPN", Port: 51821, Icon: "wireguard"},
	"fusionauth-app":      {Name: "FusionAuth", Nickname: "Login / Auth", Port: 9011, Icon: "fusionauth"},
	"authentik":           {Name: "authentik", Nickname: "Login / Auth", Port: 9000},
	"keycloak":            {Name: "Keycloak", Nickname: "Login / Auth", Port: 8080},
	"osrm-backend":        {Name: "OSRM", Nickname: "Routing Engine", Port: 5000},
	// monitoring
	"grafana":           {Name: "Grafana", Nickname: "Dashboards", Port: 3000},
	"prometheus":        {Name: "Prometheus", Nickname: "Metrics DB", Port: 9090},
	"cadvisor":          {Name: "cAdvisor", Nickname: "Container Metrics", Port: 8080},
	"loki":              {Name: "Loki", Nickname: "Logs", Helper: true},
	"node-exporter":     {Name: "Node Exporter", Nickname: "Host Metrics", Helper: true, HelperOf: "prometheus", Icon: "prometheus"},
	"snmp-exporter":     {Name: "SNMP Exporter", Nickname: "Network Metrics", Helper: true, HelperOf: "prometheus", Icon: "prometheus"},
	"blackbox-exporter": {Name: "Blackbox Exporter", Helper: true, HelperOf: "prometheus", Icon: "prometheus"},
	"socat":             {Name: "socat", Nickname: "Port Forwarder", Helper: true, HelperOf: "prometheus"},
	"watchtower":        {Name: "Watchtower", Nickname: "Auto Updates", Helper: true},
	"thebar":            {Name: "TheBar", Nickname: "This dashboard"},
	// databases / caches
	"postgres":      {Name: "PostgreSQL", Nickname: "Database", Helper: true},
	"postgis":       {Name: "PostGIS", Nickname: "Database", Helper: true, Icon: "postgres"},
	"mariadb":       {Name: "MariaDB", Nickname: "Database", Helper: true},
	"mysql":         {Name: "MySQL", Nickname: "Database", Helper: true},
	"mongo":         {Name: "MongoDB", Nickname: "Database", Helper: true, Icon: "mongodb"},
	"redis":         {Name: "Redis", Nickname: "Cache", Helper: true},
	"valkey":        {Name: "Valkey", Nickname: "Cache", Helper: true},
	"memcached":     {Name: "Memcached", Nickname: "Cache", Helper: true},
	"elasticsearch": {Name: "Elasticsearch", Nickname: "Search", Helper: true},
}

// imageSlug turns "lscr.io/linuxserver/radarr:latest" into "radarr".
func imageSlug(image string) string {
	s := image
	if i := strings.Index(s, "@"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.Index(s, ":"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

// matchKnown finds a known app for the container. The image is the strongest
// signal; postgres variants like "immich-app/postgres" also match.
func matchKnown(image, service string) (string, KnownApp, bool) {
	slug := imageSlug(image)
	try := []string{slug, service}
	// strip common suffixes/prefixes: "fusionauth-app" -> "fusionauth"
	for _, suf := range []string{"-server", "-app", "-backend", "-web"} {
		if strings.HasSuffix(slug, suf) {
			try = append(try, strings.TrimSuffix(slug, suf))
		}
	}
	for _, k := range try {
		if k == "" {
			continue
		}
		if a, ok := knownApps[k]; ok {
			return k, a, true
		}
	}
	// prefix match for things like "postgres-16" or "redis-stack"
	for _, k := range []string{"postgres", "mariadb", "mysql", "redis", "valkey", "mongo"} {
		if strings.HasPrefix(slug, k) {
			return k, knownApps[k], true
		}
	}
	return "", KnownApp{}, false
}
