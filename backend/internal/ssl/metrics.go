// Package ssl provides SSL certificate management functionality
// Requirements: 8.1, 8.2 - Monitoring and metrics for SSL certificates
package ssl

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus metrics for SSL certificate management
// Requirements: 8.1 - Expose certificate expiration metrics
// Requirements: 8.2 - Track provisioning success/failure rate
var (
	// ssl_provisioning_total tracks the total number of certificate provisioning attempts
	// Labels: status (success, failed), domain
	// Requirements: 8.2 - Track provisioning success/failure rate
	SSLProvisioningTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ssl",
			Name:      "provisioning_total",
			Help:      "Total number of certificate provisioning attempts",
		},
		[]string{"status"},
	)

	// ssl_certificate_expiry_days tracks the days until certificate expiry for each domain
	// Labels: domain, status
	// Requirements: 8.1 - Expose certificate expiration metrics
	// Requirements: 8.3 - Alert on certificates expiring within 14 days
	SSLCertificateExpiryDays = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "ssl",
			Name:      "certificate_expiry_days",
			Help:      "Days until certificate expiry for each domain",
		},
		[]string{"domain", "status"},
	)

	// ssl_renewal_total tracks the total number of certificate renewal attempts
	// Labels: status (success, failed)
	// Requirements: 8.2 - Track provisioning success/failure rate (includes renewals)
	SSLRenewalTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ssl",
			Name:      "renewal_total",
			Help:      "Total number of certificate renewal attempts",
		},
		[]string{"status"},
	)

	// ssl_active_certificates tracks the current number of active SSL certificates
	// Requirements: 8.1 - Expose certificate metrics
	SSLActiveCertificates = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "ssl",
			Name:      "active_certificates",
			Help:      "Number of active SSL certificates",
		},
	)


	// ssl_revocation_total tracks the total number of certificate revocations
	// Labels: status (success, failed), reason
	// Requirements: 8.5 - Log certificate operations for audit
	SSLRevocationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "ssl",
			Name:      "revocation_total",
			Help:      "Total number of certificate revocation attempts",
		},
		[]string{"status", "reason"},
	)

	// ssl_provisioning_duration_seconds tracks the duration of provisioning operations
	// Requirements: 8.4 - Alert on provisioning failures (helps identify slow provisioning)
	SSLProvisioningDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "ssl",
			Name:      "provisioning_duration_seconds",
			Help:      "Duration of certificate provisioning operations in seconds",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 10), // 1s to ~17min
		},
		[]string{"status"},
	)

	// ssl_renewal_duration_seconds tracks the duration of renewal operations
	SSLRenewalDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "ssl",
			Name:      "renewal_duration_seconds",
			Help:      "Duration of certificate renewal operations in seconds",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 10), // 1s to ~17min
		},
		[]string{"status"},
	)

	// ssl_cache_size tracks the number of certificates in the in-memory cache
	// Requirements: 9.5 - Implement certificate caching to reduce disk I/O
	SSLCacheSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "ssl",
			Name:      "cache_size",
			Help:      "Number of certificates in the in-memory cache",
		},
	)

	// ssl_cache_hits_total tracks cache hit count
	SSLCacheHitsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "ssl",
			Name:      "cache_hits_total",
			Help:      "Total number of certificate cache hits",
		},
	)

	// ssl_cache_misses_total tracks cache miss count
	SSLCacheMissesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "ssl",
			Name:      "cache_misses_total",
			Help:      "Total number of certificate cache misses",
		},
	)

	// ssl_certificates_by_status tracks certificates grouped by status
	SSLCertificatesByStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "ssl",
			Name:      "certificates_by_status",
			Help:      "Number of certificates grouped by status",
		},
		[]string{"status"},
	)

	// ssl_expiring_certificates tracks certificates expiring within various time windows
	// Requirements: 8.3 - Alert on certificates expiring within 14 days
	SSLExpiringCertificates = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "ssl",
			Name:      "expiring_certificates",
			Help:      "Number of certificates expiring within specified days",
		},
		[]string{"within_days"},
	)

	// ssl_renewal_failures tracks certificates with renewal failures
	SSLRenewalFailures = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "ssl",
			Name:      "renewal_failures",
			Help:      "Number of consecutive renewal failures per certificate",
		},
		[]string{"domain"},
	)
)

// MetricsStatus constants for labeling
const (
	MetricsStatusSuccess = "success"
	MetricsStatusFailed  = "failed"
)

// RecordProvisioningAttempt records a provisioning attempt metric
// Requirements: 8.2 - Track provisioning success/failure rate
func RecordProvisioningAttempt(success bool, duration float64) {
	status := MetricsStatusSuccess
	if !success {
		status = MetricsStatusFailed
	}
	SSLProvisioningTotal.WithLabelValues(status).Inc()
	SSLProvisioningDuration.WithLabelValues(status).Observe(duration)
}

// RecordRenewalAttempt records a renewal attempt metric
// Requirements: 8.2 - Track provisioning success/failure rate
func RecordRenewalAttempt(success bool, duration float64) {
	status := MetricsStatusSuccess
	if !success {
		status = MetricsStatusFailed
	}
	SSLRenewalTotal.WithLabelValues(status).Inc()
	SSLRenewalDuration.WithLabelValues(status).Observe(duration)
}

// RecordRevocationAttempt records a revocation attempt metric
// Requirements: 8.5 - Log certificate operations for audit
func RecordRevocationAttempt(success bool, reason string) {
	status := MetricsStatusSuccess
	if !success {
		status = MetricsStatusFailed
	}
	SSLRevocationTotal.WithLabelValues(status, reason).Inc()
}

// UpdateCertificateExpiryMetric updates the expiry days metric for a certificate
// Requirements: 8.1 - Expose certificate expiration metrics
func UpdateCertificateExpiryMetric(domain string, status string, daysUntilExpiry int) {
	SSLCertificateExpiryDays.WithLabelValues(domain, status).Set(float64(daysUntilExpiry))
}

// UpdateActiveCertificatesMetric updates the active certificates gauge
// Requirements: 8.1 - Expose certificate metrics
func UpdateActiveCertificatesMetric(count int) {
	SSLActiveCertificates.Set(float64(count))
}

// UpdateCacheMetrics updates cache-related metrics
// Requirements: 9.5 - Implement certificate caching
func UpdateCacheMetrics(size int) {
	SSLCacheSize.Set(float64(size))
}

// RecordCacheHit records a cache hit
func RecordCacheHit() {
	SSLCacheHitsTotal.Inc()
}

// RecordCacheMiss records a cache miss
func RecordCacheMiss() {
	SSLCacheMissesTotal.Inc()
}

// UpdateCertificatesByStatusMetric updates the certificates by status gauge
func UpdateCertificatesByStatusMetric(status string, count int) {
	SSLCertificatesByStatus.WithLabelValues(status).Set(float64(count))
}

// UpdateExpiringCertificatesMetric updates the expiring certificates gauge
// Requirements: 8.3 - Alert on certificates expiring within 14 days
func UpdateExpiringCertificatesMetric(withinDays string, count int) {
	SSLExpiringCertificates.WithLabelValues(withinDays).Set(float64(count))
}

// UpdateRenewalFailuresMetric updates the renewal failures gauge for a domain
func UpdateRenewalFailuresMetric(domain string, failures int) {
	SSLRenewalFailures.WithLabelValues(domain).Set(float64(failures))
}

// ClearCertificateExpiryMetric removes the expiry metric for a certificate
// Called when a certificate is deleted or revoked
func ClearCertificateExpiryMetric(domain string, status string) {
	SSLCertificateExpiryDays.DeleteLabelValues(domain, status)
}

// ClearRenewalFailuresMetric removes the renewal failures metric for a domain
func ClearRenewalFailuresMetric(domain string) {
	SSLRenewalFailures.DeleteLabelValues(domain)
}
