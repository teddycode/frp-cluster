package main

import (
	"fmt"
	"net"
	"os"
	"strings"

	alidns "github.com/alibabacloud-go/alidns-20150109/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/tea"

	"frp-cluster/internal/control"
)

type aliDNSConfig struct {
	AccessKeyID     string
	AccessKeySecret string
	DomainName      string
	RR              string
	RecordType      string
	TTL             int64
	Endpoint        string
	Line            string
}

func runAliDNSUpdate(configPath string) error {
	cfg, err := loadAliDNSConfig(configPath)
	if err != nil {
		return err
	}
	host := strings.TrimSpace(firstNonEmptyEnv("FRP_CLUSTER_DNS_HOST", cfg.RR+"."+cfg.DomainName))
	targetIP := strings.TrimSpace(firstNonEmptyEnv("FRP_CLUSTER_DNS_TARGET_IP", ""))
	if targetIP == "" {
		return fmt.Errorf("FRP_CLUSTER_DNS_TARGET_IP is required")
	}
	if parsed := net.ParseIP(targetIP); parsed == nil || parsed.To4() == nil {
		return fmt.Errorf("only IPv4 A records are supported, got %q", targetIP)
	}
	rr, domain := splitAliDNSHost(host, cfg.DomainName)
	if cfg.RR != "" {
		rr = cfg.RR
	}
	if domain == "" {
		domain = cfg.DomainName
	}
	if rr == "" || domain == "" {
		return fmt.Errorf("DNS host %q does not match configured domain %q", host, cfg.DomainName)
	}
	client, err := alidns.NewClient(&openapi.Config{
		AccessKeyId:     tea.String(cfg.AccessKeyID),
		AccessKeySecret: tea.String(cfg.AccessKeySecret),
		Endpoint:        tea.String(firstNonEmpty(cfg.Endpoint, "alidns.cn-hangzhou.aliyuncs.com")),
	})
	if err != nil {
		return err
	}
	searchMode := "ADVANCED"
	pageNumber := int64(1)
	pageSize := int64(20)
	recordType := firstNonEmpty(cfg.RecordType, "A")
	describe, err := client.DescribeDomainRecords(&alidns.DescribeDomainRecordsRequest{
		DomainName: tea.String(domain),
		RRKeyWord:  tea.String(rr),
		Type:       tea.String(recordType),
		SearchMode: tea.String(searchMode),
		PageNumber: tea.Int64(pageNumber),
		PageSize:   tea.Int64(pageSize),
	})
	if err != nil {
		return err
	}
	recordID := ""
	if describe != nil && describe.Body != nil && describe.Body.DomainRecords != nil {
		for _, record := range describe.Body.DomainRecords.Record {
			if record == nil {
				continue
			}
			if strings.EqualFold(tea.StringValue(record.RR), rr) && strings.EqualFold(tea.StringValue(record.Type), recordType) {
				recordID = tea.StringValue(record.RecordId)
				break
			}
		}
	}
	if recordID == "" {
		add := &alidns.AddDomainRecordRequest{
			DomainName: tea.String(domain),
			RR:         tea.String(rr),
			Type:       tea.String(recordType),
			Value:      tea.String(targetIP),
		}
		if cfg.TTL > 0 {
			add.TTL = tea.Int64(cfg.TTL)
		}
		if cfg.Line != "" {
			add.Line = tea.String(cfg.Line)
		}
		created, err := client.AddDomainRecord(add)
		if err != nil {
			return err
		}
		if created != nil && created.Body != nil {
			recordID = tea.StringValue(created.Body.RecordId)
		}
		fmt.Printf("alidns created %s.%s %s -> %s record_id=%s\n", rr, domain, recordType, targetIP, recordID)
		return nil
	}
	update := &alidns.UpdateDomainRecordRequest{
		RecordId: tea.String(recordID),
		RR:       tea.String(rr),
		Type:     tea.String(recordType),
		Value:    tea.String(targetIP),
	}
	if cfg.TTL > 0 {
		update.TTL = tea.Int64(cfg.TTL)
	}
	if cfg.Line != "" {
		update.Line = tea.String(cfg.Line)
	}
	if _, err := client.UpdateDomainRecord(update); err != nil {
		return err
	}
	fmt.Printf("alidns updated %s.%s %s -> %s record_id=%s\n", rr, domain, recordType, targetIP, recordID)
	return nil
}

func loadAliDNSConfig(path string) (aliDNSConfig, error) {
	values, err := control.ReadEnvFile(path)
	if err != nil {
		return aliDNSConfig{}, err
	}
	cfg := aliDNSConfig{
		AccessKeyID:     strings.TrimSpace(values["ALIDNS_ACCESS_KEY_ID"]),
		AccessKeySecret: strings.TrimSpace(values["ALIDNS_ACCESS_KEY_SECRET"]),
		DomainName:      strings.TrimSpace(values["ALIDNS_DOMAIN_NAME"]),
		RR:              strings.TrimSpace(values["ALIDNS_RR"]),
		RecordType:      strings.TrimSpace(firstNonEmpty(values["ALIDNS_RECORD_TYPE"], "A")),
		Endpoint:        strings.TrimSpace(values["ALIDNS_ENDPOINT"]),
		Line:            strings.TrimSpace(values["ALIDNS_LINE"]),
	}
	if ttlRaw := strings.TrimSpace(values["ALIDNS_TTL"]); ttlRaw != "" {
		var ttl int64
		if _, err := fmt.Sscanf(ttlRaw, "%d", &ttl); err != nil || ttl < 0 {
			return aliDNSConfig{}, fmt.Errorf("invalid ALIDNS_TTL %q", ttlRaw)
		}
		cfg.TTL = ttl
	}
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return aliDNSConfig{}, fmt.Errorf("ALIDNS_ACCESS_KEY_ID and ALIDNS_ACCESS_KEY_SECRET are required")
	}
	if cfg.DomainName == "" {
		return aliDNSConfig{}, fmt.Errorf("ALIDNS_DOMAIN_NAME is required")
	}
	return cfg, nil
}

func splitAliDNSHost(host, configuredDomain string) (string, string) {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	configuredDomain = strings.TrimSuffix(strings.TrimSpace(configuredDomain), ".")
	if configuredDomain != "" {
		if strings.EqualFold(host, configuredDomain) {
			return "@", configuredDomain
		}
		suffix := "." + configuredDomain
		if strings.HasSuffix(strings.ToLower(host), strings.ToLower(suffix)) {
			rr := strings.TrimSuffix(host, suffix)
			if rr == "" {
				rr = "@"
			}
			return rr, configuredDomain
		}
	}
	parts := strings.Split(host, ".")
	if len(parts) < 3 {
		return "", configuredDomain
	}
	return strings.Join(parts[:len(parts)-2], "."), strings.Join(parts[len(parts)-2:], ".")
}

func firstNonEmptyEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value != "" {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
