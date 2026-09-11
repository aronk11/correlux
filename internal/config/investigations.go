package config

// SavedInvestigation stores navigation and scope, never fetched cluster data.
type SavedInvestigation struct {
	Name            string `json:"name" yaml:"name"`
	Context         string `json:"context" yaml:"context"`
	Namespace       string `json:"namespace" yaml:"namespace"`
	AllNamespaces   bool   `json:"allNamespaces,omitempty" yaml:"allNamespaces,omitempty"`
	View            string `json:"view" yaml:"view"`
	FleetGroup      string `json:"fleetGroup,omitempty" yaml:"fleetGroup,omitempty"`
	Filter          string `json:"filter,omitempty" yaml:"filter,omitempty"`
	Application     string `json:"application,omitempty" yaml:"application,omitempty"`
	Kind            string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Resource        string `json:"resource,omitempty" yaml:"resource,omitempty"`
	Object          string `json:"object,omitempty" yaml:"object,omitempty"`
	Mode            string `json:"mode,omitempty" yaml:"mode,omitempty"`
	SubjectResource string `json:"subjectResource,omitempty" yaml:"subjectResource,omitempty"`
	SourceNamespace string `json:"sourceNamespace,omitempty" yaml:"sourceNamespace,omitempty"`
	SourcePod       string `json:"sourcePod,omitempty" yaml:"sourcePod,omitempty"`
	TargetContext   string `json:"targetContext,omitempty" yaml:"targetContext,omitempty"`
	TargetNamespace string `json:"targetNamespace,omitempty" yaml:"targetNamespace,omitempty"`
	TargetName      string `json:"targetName,omitempty" yaml:"targetName,omitempty"`
}
