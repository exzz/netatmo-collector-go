# netatmo-collector-go
Collect metrics from Netatmo API and push them into influx database

## Quickstart

- [Create a new netatmo app](https://dev.netatmo.com/dev/createapp)
- Clone repo
- Download gdm dependencies manager ```go get github.com/sparrc/gdm```
- Download dependencies ```gdm restore```
- Edit sample.conf
- Run ```go run netatmo-collector.go -f sample.conf -d```
