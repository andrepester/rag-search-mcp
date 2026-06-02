package observability

const (
	StatusOK    = "ok"
	StatusError = "error"
)

type DependencyStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Hint   string `json:"hint,omitempty"`
}

type ReadinessReport struct {
	Status       string             `json:"status"`
	Dependencies []DependencyStatus `json:"dependencies"`
}

func NewReadinessReport(dependencies []DependencyStatus) ReadinessReport {
	status := StatusOK
	for _, dependency := range dependencies {
		if dependency.Status != StatusOK {
			status = StatusError
			break
		}
	}
	return ReadinessReport{Status: status, Dependencies: dependencies}
}

func (r ReadinessReport) Ready() bool {
	return r.Status == StatusOK
}
