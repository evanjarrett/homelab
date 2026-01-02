# Security & Infrastructure TODO

## High Priority Security Enhancements

### Runtime Security Monitoring
- [ ] **Falco** - Deploy runtime anomaly detection
  - Monitor container escapes, privilege escalations
  - Detect suspicious file access patterns
  - Alert on network anomalies
  - Helm chart: `falcosecurity/falco`

### Policy Enforcement
- [ ] **OPA Gatekeeper** - Policy-as-code enforcement
  - Prevent deployment of insecure configurations
  - Enforce resource limits, security contexts
  - Block non-compliant images
  - Custom policies for homelab requirements

### Compliance & Benchmarking
- [x] **CIS Kubernetes Benchmark** - Security configuration audit
  - **EVALUATED**: `kube-bench` not suitable for Talos Linux
  - Most checks fail due to missing traditional filesystem paths in Talos
  - Talos already hardens the OS layer that kubebench audits
  - Focus on manual RBAC reviews and policy validation instead
  - Alternative: Use Falco + OPA for runtime security and policy enforcement

## Medium Priority Improvements

### Istio Service Mesh Security
- [ ] **mTLS Enforcement** - Encrypt all pod-to-pod traffic
  - Enable strict mTLS across namespaces
  - Less critical given network policies but adds defense-in-depth
  - Monitor with Kiali for service mesh observability

### Container Image Security
- [ ] **Image Vulnerability Scanning** 
  - Integrate with existing Renovate bot for image updates
  - Consider tools like Trivy for scanning
  - Image signature verification (cosign)

### Backup & Disaster Recovery
- [x] **Longhorn PVC Backups** - ✅ COMPLETED
  - Configured Longhorn backups to TrueNAS (NFS) at 192.168.1.100
  - Daily automatic backups at 2 AM with 3-day retention
  - Multiple completed backups verified (ranging from 54MB to 8.2GB)
  - Architecture: Longhorn → TrueNAS → Storj (via TrueNAS)

## Lower Priority / Nice-to-Have

### Additional Monitoring
- [ ] **Security Event Monitoring**
  - Aggregate security logs from Falco, Istio, etc.
  - Dashboard for security events in Grafana
  - Alerting for critical security events

### Advanced Access Control
- [ ] **Istio Authorization Policies**
  - Fine-grained access control beyond network policies
  - Service-to-service authentication
  - Request-level authorization

### Infrastructure Hardening
- [ ] **Secret Rotation Automation**
  - Regular rotation of certificates beyond cert-manager
  - Monitor secret age and expiration

## Research & Planning

### Personal Container Images
- [ ] **Security Review of Custom Images**
  - Review personal images for security best practices
  - Implement vulnerability scanning in CI/CD
  - Consider using distroless base images

### Network Architecture
- [ ] **Cloudflare Tunnels for WireGuard**
  - Investigate tunneling WireGuard management interface
  - Completely hide home IP for all services
  - Compare with current split-access approach

---

## Implementation Notes

- **Priority Order**: Focus on Falco → OPA → CIS benchmarking first
- **Testing Strategy**: Implement in staging/test namespace first
- **Resource Impact**: Monitor cluster resources when adding security tools
- **Documentation**: Update security documentation as tools are added

## Completed Security Improvements ✅

- Network policies with RFC1918 blocking
- Restricted Pod Security Standards for application namespaces  
- Proper security contexts with non-root users
- Gateway route restrictions by namespace
- Zero-egress policies for static content services
- Automated container image updates via Renovate bot
- Longhorn PVC backups to TrueNAS with 3-day retention
- CIS Kubernetes Benchmark evaluation (kube-bench not suitable for Talos)