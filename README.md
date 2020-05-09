# [Prometheus Alerts](https://prometheus.io/docs/alerting/alertmanager/) to [Cachet](http://cachethq.io/)

Small go based microservice to receive Prometheus Alertmanager triggers and update corresponding incidents/components in Cachet. This requires Cachet 2.4.

## Dependencies

* https://github.com/andygrunwald/cachet
* https://github.com/prometheus/alertmanager

## Configuration

You have to specify your cachet instance by setting the `CACHET_URL` environment variable. For authentication with your cachet instance you have to set the `CACHET_KEY` as environemnt variable. This is the Cachet API Token which is generated at Cachet installation time for the main user or when a new team member is added to your status page and can be found on your profile page (click your Cachet profile picture to get there). You can specify the log level by setting the `LOG_LEVEL` environment variable, as value choose one of: [debug, info, warn, error]. Per default `info` log level is used. The daemon is listening for http requests on port 80.

In your Prometheus alerts you can specify the following labels/annotations (all are optional):

| Name                           | Type       | description                                              |
| ------------------------------ | ---------- | -------------------------------------------------------- |
| cachet_incident_name           | label      | For each incoming Prometheus alert a new Cachet incident will be created. Per default the incident will have the alertname label as name, here you can specify a custom incident name. |
| cachet_component_name          | label      | Set the component name if you wish to update also the status of a component together with incicdent creation. |
| cachet_component_group_name    | label      | If the component you wish to update is in a group, provide also the group name. |
| cachet_component_status        | label      | Defines the [status](https://docs.cachethq.io/docs/component-statuses) of the component. |
| cachet_incident_message        | annotation | For each incoming Prometheus alert a new Cachet incident will be created. Per default the incident message will be taken from the summary label, here you can specify a custom incident message. | 
| cachet_incident_update_message | annotation | If the Prometheus alert will be resolved, the corresponding incident will be also resolved. Here you can specify the resolving message. If not set, "Resolved" is the default message.|

Look at the [example-prometheus.sh](example-prometheus.sh) for an example Prometheus alert.

## Alertmanager Hook

The following alert matches on label `public` set to `true` then forwards to the configured webhook:

```
route:
  receiver: cachet
  group_by: [alertname]
  group_wait: 30s
  group_interval: 1m
  repeat_interval: 1h
  routes:
    - receiver: cachet
      match:
        public: "true"
  receivers:
    - name: cachet
      webhook_configs:
        - url: "http://status-cachet:80/webhook"
```
