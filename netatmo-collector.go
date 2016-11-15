package main

import (
	"flag"
	"log"
	"time"

	toml "github.com/BurntSushi/toml"
	netatmo "github.com/exzz/netatmo-api-go"
	client "github.com/influxdata/influxdb/client/v2"
)

// Command line flag
var fConfig = flag.String("config", "/etc/netatmo-collector.conf", "configuration file to load")
var fVerbose = flag.Bool("verbose", false, "log read value")

// Configuration file
type collectorConfig struct {
	Netatmo  netatmoConfig
	Influxdb influxdbConfig
}

type netatmoConfig struct {
	ClientID      string
	ClientSecret  string
	Username      string
	Password      string
	FetchInterval int
}

type influxdbConfig struct {
	URL             string
	Username        string
	Password        string
	Database        string
	RetentionPolicy string
	Precision       string
}

var config collectorConfig

func main() {

	// Parse command line flags
	flag.Parse()

	if _, err := toml.DecodeFile(*fConfig, &config); err != nil {
		log.Fatalf("Cannot parse config file: %s", err)
	}

	log.Printf("Starting %v\n", config)

	// set up
	netatmo, err := netatmo.NewClient(netatmo.Config{
		ClientID:     config.Netatmo.ClientID,
		ClientSecret: config.Netatmo.ClientSecret,
		Username:     config.Netatmo.Username,
		Password:     config.Netatmo.Password,
	})
	if err != nil {
		log.Fatalf("Unable to connect to Netatmo API: %s", err)
	}

	influxdb, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     config.Influxdb.URL,
		Username: config.Influxdb.Username,
		Password: config.Influxdb.Password,
	})
	if err != nil {
		log.Fatalf("Unable to connect to InfluxDB: %s", err.Error())
	}

	// main loop
	tickerWrite := time.NewTicker(time.Duration(config.Netatmo.FetchInterval) * time.Second)

	for {

		select {
		case <-tickerWrite.C:

			// read data
			dc, err := netatmo.Read()
			if err != nil {
				log.Printf("Cannot fetch EnergyMonitor data: %s", err)
				continue
			}

			// create influxdb point
			bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
				Database:        config.Influxdb.Database,
				Precision:       config.Influxdb.Precision,
				RetentionPolicy: config.Influxdb.RetentionPolicy,
			})

			for _, station := range dc.Stations() {
				for _, module := range station.Modules() {
					tags := map[string]string{"station": station.StationName, "module": module.ModuleName}

					ts, data := module.Data()
					for dataType, value := range data {
						fields := map[string]interface{}{
							dataType: value,
						}

						pt, err := client.NewPoint("netatmo", tags, fields, time.Unix(int64(ts), 0))
						if err != nil {
							log.Printf("Cannot create infludb point: %s", err.Error())
							continue
						}
						bp.AddPoint(pt)

						// log read value
						if *fVerbose {
							log.Printf("%s %s %s %s %v", station.StationName, module.ModuleName, dataType, value, ts)
						}
					}
				}
			}

			// write infludb point
			err = influxdb.Write(bp)
			if err != nil {
				log.Printf("Cannot write infludb point: %s", err.Error())
				continue
			}
		}
	}
}
