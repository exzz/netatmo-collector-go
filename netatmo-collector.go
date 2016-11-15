package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	toml "github.com/BurntSushi/toml"
	netatmo "github.com/exzz/netatmo-api-go"
	client "github.com/influxdata/influxdb/client/v2"
)

// Command line flag
var fConfig = flag.String("f", "", "Configuration file")
var fDebug = flag.Bool("d", false, "Verbose")

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
	if *fConfig == "" {
		fmt.Printf("Missing required argument -f\n")
		os.Exit(0)
	}

	if _, err := toml.DecodeFile(*fConfig, &config); err != nil {
		fmt.Printf("Cannot parse config file: %s\n", err)
		os.Exit(1)
	}

	fmt.Printf("Starting\n")

	// set up
	netatmo, err := netatmo.NewClient(netatmo.Config{
		ClientID:     config.Netatmo.ClientID,
		ClientSecret: config.Netatmo.ClientSecret,
		Username:     config.Netatmo.Username,
		Password:     config.Netatmo.Password,
	})
	if err != nil {
		fmt.Printf("Unable to connect to Netatmo API: %s\n", err)
		os.Exit(1)
	}

	influxdb, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     config.Influxdb.URL,
		Username: config.Influxdb.Username,
		Password: config.Influxdb.Password,
	})
	if err != nil {
		fmt.Printf("Unable to connect to InfluxDB: %s\n", err.Error())
		os.Exit(1)
	}

	// main loop
	tickerWrite := time.NewTicker(time.Duration(config.Netatmo.FetchInterval) * time.Second)

	for {

		select {
		case <-tickerWrite.C:

			// read data
			dc, err := netatmo.Read()
			if err != nil {
				fmt.Printf("Cannot fetch EnergyMonitor data: %s\n", err)
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
							fmt.Printf("Cannot create infludb point: %s\n", err.Error())
							continue
						}
						bp.AddPoint(pt)

						// log read value
						if *fDebug {
							fmt.Printf("%s %s %s %s %v\n", station.StationName, module.ModuleName, dataType, value, ts)
						}
					}
				}
			}

			// write infludb point
			err = influxdb.Write(bp)
			if err != nil {
				fmt.Printf("Cannot write infludb point: %s\n", err.Error())
				continue
			}
		}
	}
}
