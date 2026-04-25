package service

import (
	"embed"
	"net"
	"strings"
	"sync"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
	"github.com/rs/zerolog"
)

//go:embed ip2region_v4.xdb
var ip2regionFS embed.FS

var (
	geoOnce    sync.Once
	geoSearcher *xdb.Searcher
	geoErr     error
)

// InitGeoCheck loads the ip2region database eagerly at startup.
// Must be called from server/main.go during initialization.
func InitGeoCheck(logger *zerolog.Logger) {
	geoOnce.Do(func() {
		buff, err := xdb.LoadContentFromFS(ip2regionFS, "ip2region_v4.xdb")
		if err != nil {
			geoErr = err
			logger.Warn().Err(err).Msg("ip2region DB failed to load; geo-checking disabled, all emails allowed")
			return
		}
		geoSearcher, err = xdb.NewWithBuffer(xdb.IPv4, buff)
		if err != nil {
			geoErr = err
			logger.Warn().Err(err).Msg("ip2region DB searcher failed; geo-checking disabled, all emails allowed")
			return
		}
		logger.Info().Msg("ip2region geo DB loaded successfully")
	})
}

// isChineseIP checks if a single IP address belongs to China using ip2region.
// Returns true (fail-open) if the geo DB is unavailable.
func isChineseIP(ipStr string) bool {
	if geoSearcher == nil {
		return true
	}
	region, err := geoSearcher.Search(ipStr)
	if err != nil {
		return true
	}
	return strings.HasPrefix(region, "中国")
}

// isChineseEmailDomain checks if the email domain's MX servers are located in China.
// Returns true if at least one MX IP is in China, or if DNS/geo lookup fails (fail-open).
// Note: only IPv4 MX addresses are checked; IPv6-only MX servers are treated as non-Chinese.
func isChineseEmailDomain(email string) bool {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return true
	}
	domain := parts[1]

	mxRecords, err := net.LookupMX(domain)
	if err != nil || len(mxRecords) == 0 {
		return true
	}

	for _, mx := range mxRecords {
		ips, err := net.LookupIP(mx.Host)
		if err != nil {
			continue
		}
		for _, ip := range ips {
			if ipv4 := ip.To4(); ipv4 != nil && isChineseIP(ipv4.String()) {
				return true
			}
		}
	}

	return false
}
