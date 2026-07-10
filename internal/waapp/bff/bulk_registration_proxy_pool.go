package bff

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/byte-v-forge/wa-app/internal/waapp/bulkregistration"
	"github.com/byte-v-forge/wa-app/internal/waapp/wacore"
)

const bulkRegistrationProxyPoolMultiplier = 6

type bulkRegistrationProxyPool struct {
	mu sync.Mutex

	taskID      string
	countryCode string
	planned     int
	resolver    *registrationProxyResolver

	initialized bool
	candidates  []registrationProxyCandidate
	next        int
	assigned    int
	duplicates  int
	rejected    map[string]int
}

func newBulkRegistrationProxyPool(task bulkregistration.Task, resolver *registrationProxyResolver) *bulkRegistrationProxyPool {
	return &bulkRegistrationProxyPool{
		taskID:      task.TaskID,
		countryCode: normalizeBulkCountry(task.CountryISO2),
		planned:     task.TargetCount * bulkRegistrationProxyPoolMultiplier,
		resolver:    resolver,
		rejected:    map[string]int{},
	}
}

func (p *bulkRegistrationProxyPool) lease(ctx context.Context, itemID string) (*wacore.WAProxyRoute, error) {
	if p == nil || p.resolver == nil || !p.resolver.enabled() {
		return nil, nil
	}
	for {
		candidate, candidateIndex, ok := p.nextCandidate(ctx)
		if !ok {
			if p.resolver.config.Fallback == "direct" {
				route := wacore.WAProxyRoute{ProxyMode: waProxyModeDirect, CountryCode: p.countryCode, Source: waProxySourceDirect, PolicyMode: registrationProxyModeDedicated}
				return &route, nil
			}
			return nil, fmt.Errorf("registration proxy candidate pool exhausted (%s)", p.summary())
		}
		route, err := p.resolver.candidateRoute(candidate, p.countryCode, p.taskID+":"+itemID+":"+fmt.Sprint(candidateIndex))
		if err != nil {
			p.recordRejection(registrationProxyCandidateFailureKind(err))
			continue
		}
		if err := p.resolver.egressChecker.check(ctx, route, p.countryCode); err != nil {
			p.recordRejection(registrationProxyCandidateFailureKind(err))
			continue
		}
		p.mu.Lock()
		p.assigned++
		p.mu.Unlock()
		return &route, nil
	}
}

func (p *bulkRegistrationProxyPool) nextCandidate(ctx context.Context) (registrationProxyCandidate, int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.initialized {
		p.initialized = true
		candidateSet, err := p.resolver.candidates(ctx, p.planned)
		for reason, count := range candidateSet.rejected {
			p.rejected[reason] += count
		}
		p.duplicates += candidateSet.duplicateCount
		if err != nil && len(candidateSet.rejected) == 0 {
			p.rejected[registrationProxyCandidateFailureKind(err)]++
		}
		p.candidates = candidateSet.candidates
	}
	if p.next >= len(p.candidates) {
		return registrationProxyCandidate{}, 0, false
	}
	index := p.next
	candidate := p.candidates[index]
	p.next++
	return candidate, index, true
}

func (p *bulkRegistrationProxyPool) recordRejection(reason string) {
	p.mu.Lock()
	p.rejected[reason]++
	p.mu.Unlock()
}

func (p *bulkRegistrationProxyPool) summary() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	keys := make([]string, 0, len(p.rejected))
	for key := range p.rejected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{
		"planned=" + fmt.Sprint(p.planned),
		"candidates=" + fmt.Sprint(len(p.candidates)),
		"duplicates=" + fmt.Sprint(p.duplicates),
		"assigned=" + fmt.Sprint(p.assigned),
		"remaining=" + fmt.Sprint(len(p.candidates)-p.next),
	}
	for _, key := range keys {
		parts = append(parts, key+"="+fmt.Sprint(p.rejected[key]))
	}
	return strings.Join(parts, ",")
}

func registrationProxyCandidateFailureKind(err error) string {
	message := strings.ToLower(strings.TrimSpace(fmt.Sprint(err)))
	switch {
	case strings.Contains(message, "invalid node"):
		return "invalid_node"
	case strings.Contains(message, "country mismatch"):
		return "egress_country_mismatch"
	case strings.Contains(message, "invalid data"):
		return "egress_invalid_response"
	case strings.Contains(message, "egress check failed"):
		return "egress_request_failed"
	default:
		return "source_request_failed"
	}
}
