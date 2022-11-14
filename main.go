package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/andygrunwald/cachet"
	logging "github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/alertmanager/template"
)

type alerts struct {
	client    *cachet.Client
	incidents map[string]*cachet.Incident
	mutex     sync.Mutex
}

var logger logging.Logger

func (alt *alerts) searchCachetComponentID(component, componentGroup string) (bool, cachet.Component, error) {
	emptyItem := cachet.Component{}
	filter := &cachet.ComponentGroupsQueryParams{
		Name: componentGroup,
	}

	level.Debug(logger).Log("msg", "Searching for all component groups with name="+componentGroup)
	groupResponse, response, err := alt.client.ComponentGroups.GetAll(filter)
	if err != nil {
		level.Error(logger).Log("msg", "Error searching component groups: "+err.Error(), "response", response)
		return false, emptyItem, err
	}

	if groupResponse.Meta.Pagination.Count == 0 {
		return false, emptyItem, errors.New("Did not find group with name=" + componentGroup)
	} else if groupResponse.Meta.Pagination.Count > 1 {
		return false, emptyItem, errors.New("Did find more than one group with name=" + componentGroup)
	} else {
		level.Debug(logger).Log("msg", "Did find one group with name="+componentGroup)
	}

	for _, v := range groupResponse.ComponentGroups {
		for _, j := range v.EnabledComponents {
			if j.Name == component {
				return true, j, nil
			}
			// TODO: check if more than one component will be found
		}
	}
	return false, emptyItem, errors.New("Did not found component")
}

func (alt *alerts) cachetAlert(component, componentGroup, status string, alertLabels map[string]string, alertAnnotations map[string]string) {

	level.Debug(logger).Log("msg", "Processing alert="+alertLabels["alertname"])

	var componentID int
	var withComponentID bool
	var cachetComponent cachet.Component
	var name string
	var incidentUpdateMessage string
	var message string
	var err error

	if alertLabels["cachet_incident_name"] == "" {
		name = alertLabels["alertname"]
	} else {
		name = alertLabels["cachet_incident_name"]
	}
	level.Debug(logger).Log("msg", "Set incident name to="+name)

	if alertAnnotations["cachet_incident_message"] == "" {
		message = alertAnnotations["summary"]
		if message == "" {
			message = name
		}
	} else {
		message = alertAnnotations["cachet_incident_message"]
	}
	level.Debug(logger).Log("msg", "Set incident message to="+message)

	if alertAnnotations["cachet_incident_update_message"] == "" {
		incidentUpdateMessage = "Resolved"
	} else {
		incidentUpdateMessage = alertAnnotations["cachet_incident_update_message"]
	}
	level.Debug(logger).Log("msg", "Set incident update message to="+incidentUpdateMessage)

	withComponentID, cachetComponent, err = alt.searchCachetComponentID(component, componentGroup)
	if err != nil {
		level.Warn(logger).Log("msg", "Error looking for corresponding component ID. "+err.Error()+". Updating component status will be skipped.")
	}

	if withComponentID {
		componentID = cachetComponent.ID
		level.Debug(logger).Log("msg", "Found component ID="+strconv.Itoa(componentID)+" for alert="+alertLabels["alertname"])
	} else {
		level.Debug(logger).Log("msg", "Did not find component ID for alert="+alertLabels["alertname"]+". Updating component status will be skipped.")
	}

	if _, ok := alt.incidents[name]; ok {
		if strings.ToUpper(status) == "RESOLVED" {
			level.Info(logger).Log("msg", "Resolving alert="+name)

			incidentUpdate := &cachet.IncidentUpdate{
				IncidentID:  alt.incidents[name].ID,
				Status:      4,
				HumanStatus: "Resolved",
				Message:     incidentUpdateMessage,
			}

			level.Debug(logger).Log("msg", "Creating incident update with input="+fmt.Sprintf("%v", incidentUpdate))
			_, response, err := alt.client.IncidentUpdates.Create(alt.incidents[name].ID, incidentUpdate)
			if err != nil {
				level.Error(logger).Log("msg", "Error creating incident update: "+err.Error(), "response", response)
				return
			}

			level.Debug(logger).Log("msg", "Deleting incident="+name+" from internal incidents map structure")
			alt.mutex.Lock()
			delete(alt.incidents, name)
			alt.mutex.Unlock()

			// Update component status to operational, if no other open incidents are pointing to given component ID
			if withComponentID {
				filter := &cachet.IncidentsQueryParams{
					ComponentID: componentID,
				}
				level.Debug(logger).Log("msg", "Looking for all incidents which are pointing to component ID="+strconv.Itoa(componentID))
				incidentResponse, response, err := alt.client.Incidents.GetAll(filter)
				if err != nil {
					level.Error(logger).Log("msg", "Error getting all incidents: "+err.Error(), "response", response)
					return
				}

				var i int
				level.Debug(logger).Log("msg", "Counting all open (status != 4) incidents which are pointing to component ID="+strconv.Itoa(componentID))
				for _, v := range incidentResponse.Incidents {
					if v.Status != 4 {
						i++
					}
				}
				level.Debug(logger).Log("msg", "Counted "+strconv.Itoa(i)+" open incidents for component ID="+strconv.Itoa(componentID))
				if i == 0 {
					level.Debug(logger).Log("msg", "Updating component ID="+strconv.Itoa(componentID)+" to status 1 (operational")
					component, response, err := alt.client.Components.Get(componentID)
					if err != nil {
						level.Error(logger).Log("msg", "Error getting component object for component ID="+strconv.Itoa(componentID)+" :"+err.Error(), "response", response)
						return
					}

					component.Status = 1
					_, response, err = alt.client.Components.Update(componentID, component)
					if err != nil {
						level.Error(logger).Log("msg", "Error updating component status to 1 (operational) for component ID="+strconv.Itoa(componentID)+" :"+err.Error(), "response", response)
						return
					}
				} else {
					level.Debug(logger).Log("msg", "Do not updating component ID="+strconv.Itoa(componentID)+" to status 1 (operational), because multiple still open incidents are pointing to it")
				}
			}
		} else {
			level.Info(logger).Log("msg", "Alert="+name+" already reported, skipping incident creation.")
		}
		return
	}

	level.Debug(logger).Log("msg", "Preparing incident creation for alert="+alertLabels["alertname"])

	incident := &cachet.Incident{
		Name:    name,
		Message: message,
		Status:  cachet.IncidentStatusInvestigating,
	}

	// Update component status only if the current component status is lower than desired component status
	if withComponentID {
		level.Debug(logger).Log("msg", "Looking for component object for component ID="+strconv.Itoa(componentID))

		component, response, err := alt.client.Components.Get(componentID)
		if err != nil {
			level.Error(logger).Log("msg", "Error getting component object for component ID="+strconv.Itoa(componentID)+" :"+err.Error(), "response", response)
			return
		}

		actualComponentStatus := component.Status
		incident.ComponentID = component.ID

		if cachetComponentStatus := alertLabels["cachet_component_status"]; cachetComponentStatus == "" && actualComponentStatus < 4 {
			level.Debug(logger).Log("msg", "Label cachet_component_status is not set in alert labels")
			level.Debug(logger).Log("msg", "Setting incidents component status variable to 4 (Major Outage), because actual component status is="+strconv.Itoa(actualComponentStatus)+" lower")
			incident.ComponentStatus = 4
		} else {
			level.Debug(logger).Log("msg", "Label cachet_component_status is set in alert labels to="+cachetComponentStatus)
			desiredComponentStatus, err := strconv.Atoi(cachetComponentStatus)
			if err != nil {
				level.Error(logger).Log("msg", "Error converting component status from ascii to integer:"+err.Error())
				return
			}
			if desiredComponentStatus > actualComponentStatus {
				level.Debug(logger).Log("msg", "Setting incidents component status to="+strconv.Itoa(desiredComponentStatus)+" ,because actual component status="+strconv.Itoa(actualComponentStatus)+"is lower")
				incident.ComponentStatus = desiredComponentStatus
			} else {
				level.Debug(logger).Log("msg", "The current component status is kept, because the desired status is the same")
				incident.ComponentStatus = actualComponentStatus
			}
		}
	}

	level.Debug(logger).Log("msg", "Creating new incident with input="+fmt.Sprintf("%v", incident))
	newIncident, response, err := alt.client.Incidents.Create(incident)
	if err != nil {
		level.Error(logger).Log("msg", "Error creating new incident:"+err.Error(), "response", response)
		return
	}

	level.Info(logger).Log("msg", "Created new incident with name="+newIncident.Name+" and incident ID="+strconv.Itoa(newIncident.ID))

	level.Debug(logger).Log("msg", "Adding incident="+name+" to internal incidents map structure")
	alt.mutex.Lock()
	alt.incidents[name] = newIncident
	alt.mutex.Unlock()
}

func (alt *alerts) prometheusAlert(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	level.Info(logger).Log("msg", "Receiving alert...")
	component := r.URL.Query().Get("component")
	componentGroup := r.URL.Query().Get("componentGroup")
	data := template.Data{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		level.Error(logger).Log("msg", "Error decoding alert: "+err.Error(), "body", r.Body)
		return
	}
	status := data.Status
	level.Debug(logger).Log("msg", "Alerts status:"+data.Status)
	for _, alert := range data.Alerts {
		level.Debug(logger).Log("msg", "Alert: status="+alert.Status+" labels="+fmt.Sprintf("%v", alert.Labels)+" annotations="+fmt.Sprintf("%v", alert.Annotations))
		alt.cachetAlert(component, componentGroup, status, alert.Labels, alert.Annotations)
	}
}

func health(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Alive")
}

func main() {

	logger = logging.NewLogfmtLogger(os.Stderr)

	logLevel := os.Getenv("LOG_LEVEL")

	switch strings.ToUpper(logLevel) {
	case "DEBUG":
		logger = level.NewFilter(logger, level.AllowDebug())
	case "INFO":
		logger = level.NewFilter(logger, level.AllowInfo())
	case "WARN":
		logger = level.NewFilter(logger, level.AllowWarn())
	case "ERROR":
		logger = level.NewFilter(logger, level.AllowError())
	default:
		logger = level.NewFilter(logger, level.AllowInfo())
	}

	logger = logging.With(logger, "ts", logging.DefaultTimestampUTC, "caller", logging.DefaultCaller)

	level.Info(logger).Log("msg", "Prometheus Cachet bridge started!")

	statusPage := os.Getenv("CACHET_URL")
	if len(statusPage) == 0 {
		panic(level.Error(logger).Log("CACHET_URL must not be empty."))
	}
	client, err := cachet.NewClient(statusPage, nil)
	if err != nil {
		panic(level.Error(logger).Log("msg", "Error creating new Cachet client: "+err.Error()))
	}
	apiKey := os.Getenv("CACHET_KEY")
	if len(apiKey) == 0 {
		panic(level.Error(logger).Log("CACHET_KEY must not be empty."))
	}
	client.Authentication.SetTokenAuth(apiKey)
	// client.Authentication.SetBasicAuth("test@example.com", "test123")

	alerts := alerts{incidents: make(map[string]*cachet.Incident), client: client}
	http.HandleFunc("/health", health)
	http.HandleFunc("/webhook", alerts.prometheusAlert)
	listenAddress := ":80"
	if os.Getenv("PORT") != "" {
		listenAddress = ":" + os.Getenv("PORT")
	}
	level.Info(logger).Log("msg", "Listening on", listenAddress)
	log.Fatal(http.ListenAndServe(listenAddress, nil))
}
