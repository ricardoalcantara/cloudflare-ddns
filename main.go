package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/cloudflare/cloudflare-go"
	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	logLevel, err := zerolog.ParseLevel(os.Getenv("LOG_LEVEL"))
	if err != nil {
		log.Fatal().Err(err).Send()
	}
	zerolog.SetGlobalLevel(logLevel)

	log.Info().Msg("Started")
	log.Info().Str("Interval", os.Getenv("INTERVAL")).Send()

	s := gocron.NewScheduler(time.UTC)
	s.Every(os.Getenv("INTERVAL")).Do(mainJob)

	s.StartBlocking()
	log.Info().Msg("Finished")
}

func mainJob() {
	ipv4, ipv6, err := fetchIp()
	if err != nil {
		log.Fatal().Err(err).Send()
	}

	log.Debug().Str("ipv4", ipv4).Str("ipv6", ipv6).Send()

	updateRecords(ipv4, ipv6)
}

func fetchIp() (string, string, error) {
	ipv4, ipv6, err := fetchIpFrom("https://ifconfig.me")
	if err != nil {
		log.Error().Err(err).Send()
	}

	ipv4, ipv6, err = fetchIpFrom("https://ipecho.net/plain")
	if err != nil {
		log.Error().Err(err).Send()

		return "", "", errors.New("unsable to fetch ip")
	}

	return ipv4, ipv6, nil
}

func updateRecords(ipv4, ipv6 string) error {
	zoneName := os.Getenv("ZONE_NAME")
	api, err := cloudflare.NewWithAPIToken(os.Getenv("CLOUDFLARE_API_TOKEN"))
	if err != nil {
		log.Fatal().Err(err).Send()
	}
	ctx := context.Background()

	zones, err := api.ListZones(ctx)
	if err != nil {
		return nil
	}

	for _, zone := range zones {
		if zone.Name == zoneName {
			log.Debug().Interface("zone", zone.Name).Send()
			resourceContainer := cloudflare.ResourceContainer{Identifier: zone.ID}
			dnsRecords, resultInfo, err := api.ListDNSRecords(ctx, &resourceContainer, cloudflare.ListDNSRecordsParams{})
			if err != nil {
				log.Fatal().Err(err).Send()
			}

			if resultInfo.TotalPages > 1 {
				log.Warn().Msg("There's more pages, it might not work properly")
			}

			for _, dnsRecord := range dnsRecords {
				switch dnsRecord.Type {
				case "A":
					if dnsRecord.Content == ipv4 {
						log.Info().Str("ipv4", ipv4).Msg("A record is up to date")
					} else {
						_, err := api.UpdateDNSRecord(ctx, &resourceContainer, cloudflare.UpdateDNSRecordParams{
							ID:      dnsRecord.ID,
							Content: ipv4,
						})
						if err != nil {
							log.Error().Err(err).Msg("Could not update A record")
						} else {
							log.Info().Str("ipv4", ipv4).Msg("Updated A record")
						}
					}
				case "AAAA":
					if dnsRecord.Content == ipv6 {
						log.Info().Str("ipv6", ipv6).Msg("AAAA record is up to date")
					} else {
						_, err := api.UpdateDNSRecord(ctx, &resourceContainer, cloudflare.UpdateDNSRecordParams{
							ID:      dnsRecord.ID,
							Content: ipv6,
						})
						if err != nil {
							log.Error().Err(err).Msg("Could not update AAAA record")
						} else {
							log.Info().Str("ipv6", ipv6).Msg("Updated AAAA record")
						}
					}
				}
			}
		}
	}

	return nil
}

func fetchIpFrom(from string) (string, string, error) {
	ipv4, err := fetchContent(from, "tcp4")
	if err != nil {
		return "", "", err
	}
	ipv6, err := fetchContent(from, "tcp6")
	if err != nil {
		return "", "", err
	}

	return ipv4, ipv6, nil
}

func fetchContent(url string, sNetwork string) (string, error) {
	client := &http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return net.Dial(sNetwork, addr)
			},
		},
	}
	response, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	content := string(body)
	return content, nil
}
