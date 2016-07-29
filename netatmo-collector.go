package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/davecgh/go-spew/spew"
	"github.com/exzz/netatmo-api-go"
	"github.com/influxdata/influxdb/client"

	//"github.com/davecgh/go-spew/spew"
	log "gopkg.in/inconshreveable/log15.v2"
)

// Command line flag
var fConfig = flag.String("config", "/etc/netatmo-collector/netatmo-collector.conf", "configuration file to load")

const usage = `Netatmo Collector, gather Netatmo metrics and store them into influxdb database
Usage:
  netatmo-collector <flags>
The flags are:
  -config <file>     configuration file to load
  -help              this message
`

// Configuration file
type collectorConfig struct {
	Netatmo  netatmoConfig
	Influxdb influxdbConfig
}

type netatmoConfig struct {
	ClientID     string
	ClientSecret string
	Username     string
	Password     string
	Interval     int
}

type influxdbConfig struct {
	Url             string
	Username        string
	Password        string
	Database        string
	RetentionPolicy string
}

var config collectorConfig

func main() {

	// Parse command line flags
	flag.Usage = func() { usageExit(0) }
	flag.Parse()

	// Load configuration file
	_, err := os.Stat(*fConfig)
	if err != nil {
		log.Crit("Config file not found ", "path", *fConfig)
		os.Exit(1)
	}
	if _, err := toml.DecodeFile(*fConfig, &config); err != nil {
		log.Crit("Cannot parse config file", "err", err)
		os.Exit(1)
	}

	// debug
	spew.Dump(config)

	// Handle OS signals
	shutdown := make(chan bool)
	signals := make(chan os.Signal)
	signal.Notify(signals, os.Interrupt)
	go func() {
		sig := <-signals
		if sig == os.Interrupt {
			close(shutdown)
		}
	}()
	log.Info("Starting")

	var wg sync.WaitGroup
	flush := make(chan bool)
	c := make(chan client.Point)

	// read netatmo metrics
	wg.Add(1)
	go netatmoGather(&wg, shutdown, flush, c)

	// write metrics to influxdb
	wg.Add(1)
	go influxdbWrite(&wg, shutdown, flush, c)

	wg.Wait()
}

func usageExit(rc int) {
	fmt.Println(usage)
	os.Exit(rc)
}

func netatmoGather(wg *sync.WaitGroup, shutdown <-chan bool, flush chan<- bool, c chan<- client.Point) error {
	defer wg.Done()
	defer log.Debug("netatmoGather exited")

	// Connect to Netatmo
	netatmoClient, err := netatmo.NewClient(netatmo.Config{
		ClientID:     config.Netatmo.ClientID,
		ClientSecret: config.Netatmo.ClientSecret,
		Username:     config.Netatmo.Username,
		Password:     config.Netatmo.Password,
	})
	if err != nil {
		log.Crit("Cannot connect to Netatmo API", "err", err)
		os.Exit(1)
	}
	log.Info("Connected to Netatmo API")

	// gather metrics every config.netatmo.interval seconds
	ticker := time.NewTicker(time.Second * time.Duration(config.Netatmo.Interval))
	defer ticker.Stop()

	for {

		devices, err := netatmoClient.Read()
		if err != nil {
			log.Error("Cannot fetch Netatmo data", "err", err)
		} else {
			for _, station := range devices.Stations() {

				for _, module := range station.Modules() {

					ts, data := module.Data()
					for dataType, value := range data {

						log.Debug("New point", "ts", ts, "field", dataType, "station", station.StationName, "module", module.ModuleName, "value", value)

						c <- client.Point{
							Measurement: dataType,
							Tags: map[string]string{
								"station": station.StationName,
								"module":  module.ModuleName,
							},
							Fields: map[string]interface{}{
								"value": value,
							},
							Time:      time.Unix(int64(ts), 0),
							Precision: "s",
						}
					}
				}
			}

			flush <- true
		}

		select {

		case <-shutdown:
			return nil

		case <-ticker.C:
			continue
		}
	}
}

func influxdbWrite(wg *sync.WaitGroup, shutdown <-chan bool, flush chan bool, c <-chan client.Point) error {
	defer wg.Done()
	defer log.Debug("influxdbWrite exited")

	// Connect to influxdb
	u, err := url.Parse(config.Influxdb.Url)
	influxdbClient, err := client.NewClient(client.Config{
		URL:      *u,
		Username: config.Influxdb.Username,
		Password: config.Influxdb.Password,
	})
	if err != nil {
		log.Crit("Cannot connect to InfluxDB", "err", err)
		os.Exit(1)
	}
	log.Info("Connected to InfluxDB")

	points := []client.Point{}

	doShutdown := false
	doFlush := false

	for {

		if doFlush {
			log.Debug("Sending metrics", "size", len(points))
			bps := client.BatchPoints{
				Points:          points,
				Database:        config.Influxdb.Database,
				RetentionPolicy: config.Influxdb.RetentionPolicy,
			}
			_, err := influxdbClient.Write(bps)
			if err != nil {
				log.Error("Cannot write points to influxDB", "err", err)
			}

			// empty buffer
			points = []client.Point{}

			doFlush = false
			log.Debug("Sent", "size", len(points))
		}

		if doShutdown {
			log.Debug("do shutdown", "size", len(points))
			return nil
		}

		select {

		case newPoints := <-c:
			points = append(points, newPoints)
			continue

		case <-shutdown:
			doFlush = true
			doShutdown = true
			continue

		case <-flush:
			doFlush = true
			continue

		}
	}
}
