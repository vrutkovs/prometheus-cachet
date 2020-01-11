#!/bin/bash

alert='
[{
  "status": "firing",
	"labels": {
		"alertname": "High Latency",
		"service":   "my-service",
		"severity":  "warning",
		"instance":  "somewhere",
		"cachet_incident_name": "Damn High k8s latency",
		"cachet_component_name": "API",
		"cachet_component_group_name": "Kubernetes",
		"cachet_component_status": "4",
    "public": "true"
	},
	"annotations": {
		"summary": "The latency is too damn high!",
		"cachet_incident_message": "The k8s API latency is too damn high!"
	},
  "generatorURL": "http://example.com"
}]'

curl -XPOST -d "$alert" "http://localhost:9093/api/v1/alerts"

echo -e "\nPress enter to resolve."
read

alert='
[{
  "status": "resolved",
	"labels": {
		"alertname": "High Latency",
		"service":   "my-service",
		"severity":  "warning",
		"instance":  "somewhere",
		"cachet_incident_name": "Damn High k8s latency",
		"cachet_component_name": "API",
		"cachet_component_group_name": "Kubernetes",
		"cachet_component_status": "4",
    "public": "true"
	},
	"annotations": {
		"summary": "The latency is too damn high!",
		"cachet_incident_message": "The k8s API latency is too damn high!"
	},
  "generatorURL": "http://example.com"
}]'

curl -XPOST -d "$alert" "http://localhost:9093/api/v1/alerts"
