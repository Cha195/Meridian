package geo

import (
	"fmt"
	"net"

	"github.com/oschwald/geoip2-golang"
)

type GeoResult struct {
	Country   string
	City      string
	Lat       float64
	Lng       float64
	Continent string
	IsLocal   bool
}

type GeoLocator struct {
	db *geoip2.Reader
}

func NewGeoLocator(dbPath string) (*GeoLocator, error) {
	db, err := geoip2.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open GeoIP database: %w", err)
	}
	return &GeoLocator{db: db}, nil
}

func (g *GeoLocator) Close() {
	g.db.Close()
}

func (g *GeoLocator) Lookup(ipStr string) GeoResult {
	ip := net.ParseIP(ipStr)
	if ip == nil || isPrivateIP(ip) {
		return GeoResult{
			Country:   "Local",
			City:      "Local",
			Continent: "Local",
			IsLocal:   true,
		}
	}

	record, err := g.db.City(ip)
	if err != nil {
		return GeoResult{IsLocal: false}
	}

	city := ""
	if len(record.City.Names) > 0 {
		city = record.City.Names["en"]
	}

	return GeoResult{
		Country:   record.Country.IsoCode,
		City:      city,
		Lat:       record.Location.Latitude,
		Lng:       record.Location.Longitude,
		Continent: record.Continent.Code,
	}
}

func isPrivateIP(ip net.IP) bool {
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"fc00::/7",
	}
	for _, cidr := range privateRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
