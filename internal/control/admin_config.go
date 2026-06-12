package control

import (
	"os"
	"strings"
)

type AliDNSSettings struct {
	AccessKeyID        string `json:"access_key_id"`
	AccessKeySecret    string `json:"access_key_secret,omitempty"`
	AccessKeySecretSet bool   `json:"access_key_secret_set"`
	DomainName         string `json:"domain_name"`
	RR                 string `json:"rr"`
	RecordType         string `json:"record_type"`
	TTL                string `json:"ttl"`
	Endpoint           string `json:"endpoint"`
	Line               string `json:"line"`
}

type AgentSettings struct {
	Interval  string `json:"interval"`
	ProbeSize string `json:"probe_size"`
}

func ReadAliDNSSettings(path string) (AliDNSSettings, error) {
	values, err := ReadEnvFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultAliDNSSettings(), nil
		}
		return AliDNSSettings{}, err
	}
	return AliDNSSettings{
		AccessKeyID:        values["ALIDNS_ACCESS_KEY_ID"],
		AccessKeySecretSet: strings.TrimSpace(values["ALIDNS_ACCESS_KEY_SECRET"]) != "",
		DomainName:         firstNonEmpty(values["ALIDNS_DOMAIN_NAME"], "buaadcl.tech"),
		RR:                 values["ALIDNS_RR"],
		RecordType:         firstNonEmpty(values["ALIDNS_RECORD_TYPE"], "A"),
		TTL:                firstNonEmpty(values["ALIDNS_TTL"], "600"),
		Endpoint:           firstNonEmpty(values["ALIDNS_ENDPOINT"], "alidns.cn-hangzhou.aliyuncs.com"),
		Line:               firstNonEmpty(values["ALIDNS_LINE"], "default"),
	}, nil
}

func WriteAliDNSSettings(path string, settings AliDNSSettings) error {
	existing, _ := ReadEnvFile(path)
	if existing == nil {
		existing = map[string]string{}
	}
	secret := strings.TrimSpace(settings.AccessKeySecret)
	if secret == "" {
		secret = existing["ALIDNS_ACCESS_KEY_SECRET"]
	}
	values := map[string]string{
		"ALIDNS_ACCESS_KEY_ID":     strings.TrimSpace(settings.AccessKeyID),
		"ALIDNS_ACCESS_KEY_SECRET": secret,
		"ALIDNS_DOMAIN_NAME":       firstNonEmpty(settings.DomainName, "buaadcl.tech"),
		"ALIDNS_RR":                strings.TrimSpace(settings.RR),
		"ALIDNS_RECORD_TYPE":       firstNonEmpty(settings.RecordType, "A"),
		"ALIDNS_TTL":               firstNonEmpty(settings.TTL, "600"),
		"ALIDNS_ENDPOINT":          firstNonEmpty(settings.Endpoint, "alidns.cn-hangzhou.aliyuncs.com"),
		"ALIDNS_LINE":              firstNonEmpty(settings.Line, "default"),
	}
	return WriteEnvFile(path, values, 0o600)
}

func defaultAliDNSSettings() AliDNSSettings {
	return AliDNSSettings{
		DomainName: "buaadcl.tech",
		RecordType: "A",
		TTL:        "600",
		Endpoint:   "alidns.cn-hangzhou.aliyuncs.com",
		Line:       "default",
	}
}

func ReadAgentSettings(path string) (AgentSettings, error) {
	values, err := ReadEnvFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AgentSettings{Interval: "30s", ProbeSize: "262144"}, nil
		}
		return AgentSettings{}, err
	}
	return AgentSettings{
		Interval:  firstNonEmpty(values["AGENT_INTERVAL"], "30s"),
		ProbeSize: firstNonEmpty(values["PROBE_SIZE"], "262144"),
	}, nil
}

func WriteAgentSettings(path string, settings AgentSettings) error {
	values, err := ReadEnvFile(path)
	if err != nil {
		return err
	}
	values["AGENT_INTERVAL"] = firstNonEmpty(settings.Interval, "30s")
	values["PROBE_SIZE"] = firstNonEmpty(settings.ProbeSize, "262144")
	return WriteEnvFile(path, values, 0o600)
}
