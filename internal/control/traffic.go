// Package control implements the frp-cluster control plane state model and APIs.
package control

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	trafficSampleInterval = time.Minute
	trafficRetention      = 7 * 24 * time.Hour
	maxTrafficSamples     = 100000
)

func normalizeTrafficCounters(traffic TrafficCounters, now time.Time) (TrafficCounters, bool) {
	if traffic.MeasuredAt.IsZero() && traffic.TotalInBytes == 0 && traffic.TotalOutBytes == 0 && traffic.CurrentConnections == 0 && len(traffic.Proxies) == 0 {
		return TrafficCounters{}, false
	}
	if traffic.MeasuredAt.IsZero() {
		traffic.MeasuredAt = now
	}
	traffic.TotalInBytes = nonNegative(traffic.TotalInBytes)
	traffic.TotalOutBytes = nonNegative(traffic.TotalOutBytes)
	traffic.CurrentConnections = nonNegative(traffic.CurrentConnections)
	traffic.Proxies = append([]ProxyTraffic(nil), traffic.Proxies...)
	for i := range traffic.Proxies {
		traffic.Proxies[i].Name = strings.TrimSpace(traffic.Proxies[i].Name)
		traffic.Proxies[i].Type = strings.TrimSpace(strings.ToLower(traffic.Proxies[i].Type))
		traffic.Proxies[i].ClientID = strings.TrimSpace(traffic.Proxies[i].ClientID)
		traffic.Proxies[i].TotalInBytes = nonNegative(traffic.Proxies[i].TotalInBytes)
		traffic.Proxies[i].TotalOutBytes = nonNegative(traffic.Proxies[i].TotalOutBytes)
		traffic.Proxies[i].CurrentConnections = nonNegative(traffic.Proxies[i].CurrentConnections)
		traffic.Proxies[i].Status = strings.TrimSpace(traffic.Proxies[i].Status)
	}
	sort.Slice(traffic.Proxies, func(i, j int) bool {
		if traffic.Proxies[i].Type != traffic.Proxies[j].Type {
			return traffic.Proxies[i].Type < traffic.Proxies[j].Type
		}
		return traffic.Proxies[i].Name < traffic.Proxies[j].Name
	})
	return traffic, true
}

func (s *Store) recordTrafficLocked(nodeID string, traffic TrafficCounters, now time.Time) bool {
	traffic, ok := normalizeTrafficCounters(traffic, now)
	if !ok {
		return false
	}
	node := s.state.Nodes[nodeID]
	if node == nil {
		return false
	}
	node.Traffic = traffic
	previous := lastTrafficSampleForNode(s.state.TrafficSamples, nodeID)
	if !previous.At.IsZero() && now.Sub(previous.At) < trafficSampleInterval {
		return true
	}
	sample := TrafficSample{
		NodeID:             nodeID,
		At:                 traffic.MeasuredAt,
		TotalInBytes:       traffic.TotalInBytes,
		TotalOutBytes:      traffic.TotalOutBytes,
		CurrentConnections: traffic.CurrentConnections,
	}
	if !previous.At.IsZero() {
		sample.DeltaInBytes = positiveDelta(previous.TotalInBytes, traffic.TotalInBytes)
		sample.DeltaOutBytes = positiveDelta(previous.TotalOutBytes, traffic.TotalOutBytes)
	}
	s.state.TrafficSamples = append(s.state.TrafficSamples, sample)
	s.pruneTrafficSamplesLocked(now)
	return true
}

func (s *Store) pruneTrafficSamplesLocked(now time.Time) {
	if len(s.state.TrafficSamples) == 0 {
		return
	}
	cutoff := now.Add(-trafficRetention)
	out := s.state.TrafficSamples[:0]
	for _, sample := range s.state.TrafficSamples {
		if sample.At.IsZero() || sample.At.Before(cutoff) {
			continue
		}
		out = append(out, sample)
	}
	if len(out) > maxTrafficSamples {
		out = out[len(out)-maxTrafficSamples:]
	}
	s.state.TrafficSamples = out
}

func (s *Store) TrafficSeries(window time.Duration) TrafficSeries {
	if window <= 0 || window > trafficRetention {
		window = 24 * time.Hour
	}
	cutoff := time.Now().UTC().Add(-window)
	s.mu.RLock()
	defer s.mu.RUnlock()

	series := TrafficSeries{Samples: make([]TrafficSample, 0)}
	nodeTotals := map[string]*NodeTraffic{}
	for _, node := range s.state.Nodes {
		traffic, ok := normalizeTrafficCounters(node.Traffic, time.Now().UTC())
		if !ok {
			continue
		}
		entry := nodeTotals[node.ID]
		if entry == nil {
			entry = &NodeTraffic{NodeID: node.ID}
			nodeTotals[node.ID] = entry
		}
		entry.Totals.TotalInBytes = traffic.TotalInBytes
		entry.Totals.TotalOutBytes = traffic.TotalOutBytes
	}
	for _, sample := range s.state.TrafficSamples {
		if sample.At.Before(cutoff) {
			continue
		}
		series.Samples = append(series.Samples, sample)
		entry := nodeTotals[sample.NodeID]
		if entry == nil {
			entry = &NodeTraffic{NodeID: sample.NodeID}
			nodeTotals[sample.NodeID] = entry
		}
		if sample.TotalInBytes > entry.Totals.TotalInBytes {
			entry.Totals.TotalInBytes = sample.TotalInBytes
		}
		if sample.TotalOutBytes > entry.Totals.TotalOutBytes {
			entry.Totals.TotalOutBytes = sample.TotalOutBytes
		}
		entry.Totals.DeltaInBytes += sample.DeltaInBytes
		entry.Totals.DeltaOutBytes += sample.DeltaOutBytes
	}
	sort.Slice(series.Samples, func(i, j int) bool {
		if series.Samples[i].At.Equal(series.Samples[j].At) {
			return series.Samples[i].NodeID < series.Samples[j].NodeID
		}
		return series.Samples[i].At.Before(series.Samples[j].At)
	})
	series.Nodes = make([]NodeTraffic, 0, len(nodeTotals))
	for _, entry := range nodeTotals {
		series.Totals.TotalInBytes += entry.Totals.TotalInBytes
		series.Totals.TotalOutBytes += entry.Totals.TotalOutBytes
		series.Totals.DeltaInBytes += entry.Totals.DeltaInBytes
		series.Totals.DeltaOutBytes += entry.Totals.DeltaOutBytes
		series.Nodes = append(series.Nodes, *entry)
	}
	sort.Slice(series.Nodes, func(i, j int) bool { return series.Nodes[i].NodeID < series.Nodes[j].NodeID })
	return series
}

func mergeTrafficSamples(local, remote []TrafficSample, now time.Time) []TrafficSample {
	cutoff := now.Add(-trafficRetention)
	byKey := map[string]TrafficSample{}
	for _, sample := range append(append([]TrafficSample(nil), local...), remote...) {
		if sample.NodeID == "" || sample.At.IsZero() || sample.At.Before(cutoff) {
			continue
		}
		key := sample.NodeID + "\x00" + strconv.FormatInt(sample.At.Unix(), 10)
		if existing, ok := byKey[key]; !ok || sample.At.After(existing.At) {
			byKey[key] = sample
		}
	}
	out := make([]TrafficSample, 0, len(byKey))
	for _, sample := range byKey {
		out = append(out, sample)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			return out[i].NodeID < out[j].NodeID
		}
		return out[i].At.Before(out[j].At)
	})
	if len(out) > maxTrafficSamples {
		out = out[len(out)-maxTrafficSamples:]
	}
	return out
}

func lastTrafficSampleForNode(samples []TrafficSample, nodeID string) TrafficSample {
	for i := len(samples) - 1; i >= 0; i-- {
		if samples[i].NodeID == nodeID {
			return samples[i]
		}
	}
	return TrafficSample{}
}

func positiveDelta(previous, current int64) int64 {
	if current <= previous {
		return 0
	}
	return current - previous
}
