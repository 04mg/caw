package quota

type Quota struct {
	Used  int    `json:"used"`
	Limit int    `json:"limit"`
	Unit  string `json:"unit,omitempty"` // "" | "percentage" | "credits" | "count"
}

type QuotaItem struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Used        int    `json:"used"`
	Limit       int    `json:"limit"`
	Unit        string `json:"unit,omitempty"`
	ResetTime   string `json:"resetTime,omitempty"`
}

type QuotaGroup struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Items       []QuotaItem `json:"items"`
}

type QuotaResponse struct {
	FiveHour Quota        `json:"fiveHour"`
	Weekly   Quota        `json:"weekly"`
	Monthly  Quota        `json:"monthly"`
	Groups   []QuotaGroup `json:"groups,omitempty"`
}

type ProviderResponse struct {
	Data  *QuotaResponse `json:"data,omitempty"`
	Error string         `json:"error,omitempty"`
}

type QuotaProvider interface {
	GetQuotas(config map[string]string) (*QuotaResponse, error)
}

var registry = make(map[string]QuotaProvider)

func RegisterProvider(name string, provider QuotaProvider) {
	registry[name] = provider
}