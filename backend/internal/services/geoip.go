package services

import (
	"encoding/json"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/oschwald/maxminddb-golang"
)

// Map projection dimensions (equirectangular viewBox 0 0 1000 500).
const (
	MapW = 1000
	MapH = 500
)

// GeoRecord is a resolved geolocation for an IP.
type GeoRecord struct {
	IP          string  `json:"ip"`
	City        string  `json:"city"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Flag        string  `json:"flag"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
}

// mmdb record shape (DB-IP City Lite / MaxMind City schema, subset we use).
type mmdbCity struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Location struct {
		Latitude  *float64 `maxminddb:"latitude"`
		Longitude *float64 `maxminddb:"longitude"`
	} `maxminddb:"location"`
}

// GeoIP performs fully-offline geolocation and holds the world map paths.
type GeoIP struct {
	reader     *maxminddb.Reader
	worldPaths []string
	worldOnce  sync.Once
	worldFile  string
}

// NewGeoIP opens the offline DB-IP database and remembers the world-paths file.
func NewGeoIP(assetsDir string) *GeoIP {
	g := &GeoIP{worldFile: filepath.Join(assetsDir, "world_paths.json")}
	mmdb := filepath.Join(assetsDir, "geoip", "dbip-city.mmdb")
	if _, err := os.Stat(mmdb); err == nil {
		if r, err := maxminddb.Open(mmdb); err == nil {
			g.reader = r
			log.Printf("geoip db loaded: %s", mmdb)
		} else {
			log.Printf("geoip unavailable: %v", err)
		}
	} else {
		log.Printf("geoip db not found at %s (run install to download)", mmdb)
	}
	return g
}

// Available reports whether the geolocation database is loaded.
func (g *GeoIP) Available() bool { return g != nil && g.reader != nil }

func isPublic(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	return true
}

func flagURL(cc string) string {
	if len(cc) == 2 && isAlpha(cc) {
		return "/flags/" + cc + ".svg"
	}
	return "/flags/xx.svg"
}

func isAlpha(s string) bool {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return s != ""
}

// Lookup resolves an IP to a GeoRecord, or nil if private/unknown/unavailable.
func (g *GeoIP) Lookup(ipStr string) *GeoRecord {
	if !g.Available() {
		return nil
	}
	ip := net.ParseIP(ipStr)
	if !isPublic(ip) {
		return nil
	}
	var rec mmdbCity
	if err := g.reader.Lookup(ip, &rec); err != nil {
		return nil
	}
	if rec.Location.Latitude == nil || rec.Location.Longitude == nil {
		return nil
	}
	cc := lower(rec.Country.ISOCode)
	return &GeoRecord{
		IP:          ipStr,
		City:        rec.City.Names["en"],
		Country:     rec.Country.Names["en"],
		CountryCode: cc,
		Flag:        flagURL(cc),
		Lat:         round4(*rec.Location.Latitude),
		Lon:         round4(*rec.Location.Longitude),
	}
}

// ToXY projects lat/lon to pixel coordinates on the equirectangular map.
func ToXY(lat, lon float64) (float64, float64) {
	x := (lon + 180.0) / 360.0 * MapW
	y := (90.0 - lat) / 180.0 * MapH
	return round1(x), round1(y)
}

// WorldPaths returns the SVG country paths (loaded once).
func (g *GeoIP) WorldPaths() []string {
	g.worldOnce.Do(func() {
		b, err := os.ReadFile(g.worldFile)
		if err != nil {
			g.worldPaths = []string{}
			return
		}
		var doc struct {
			Paths []string `json:"paths"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			g.worldPaths = []string{}
			return
		}
		g.worldPaths = doc.Paths
	})
	return g.worldPaths
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }
func round4(f float64) float64 { return math.Round(f*10000) / 10000 }

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
