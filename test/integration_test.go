// test/integration_test.go
package test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/signalnine/tasseograph/internal/collector"
	"github.com/signalnine/tasseograph/internal/config"
	"github.com/signalnine/tasseograph/internal/protocol"
)

// TestIntegrationCollectorIngest tests the full flow from HTTP request to DB storage
func TestIntegrationCollectorIngest(t *testing.T) {
	// 1. Start mock LLM server that returns a valid analysis response (OpenAI format)
	mockLLMServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("LLM: Method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("LLM: Path = %q, want /chat/completions", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": `{"status": "warning", "issues": [{"severity": "warning", "summary": "ECC error detected", "evidence": "EDAC MC0: 1 CE"}]}`,
					},
				},
			},
		})
	}))
	defer mockLLMServer.Close()

	// 2. Generate self-signed TLS certificate for the test
	tempDir := t.TempDir()
	certFile, keyFile := generateTestCert(t, tempDir)

	// 3. Create temporary SQLite database
	dbPath := filepath.Join(tempDir, "test.db")

	// 4. Create collector config
	cfg := &config.CollectorConfig{
		ListenAddr:      "127.0.0.1:0", // Use port 0 for auto-assignment
		DBPath:          dbPath,
		MaxPayloadBytes: 1 << 20, // 1 MB
		TLSCert:         certFile,
		TLSKey:          keyFile,
		APIKey:          "test-api-key",
		LLMEndpoints: []config.LLMEndpoint{
			{
				URL:    mockLLMServer.URL,
				Model:  "test-model",
				APIKey: "test-llm-key",
			},
		},
	}

	// 5. Start the collector server
	srv, err := collector.NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverErrCh := make(chan error, 1)
	serverAddrCh := make(chan string, 1)
	go func() {
		addr, err := srv.RunAndGetAddr(ctx)
		if err != nil {
			serverErrCh <- err
			return
		}
		serverAddrCh <- addr
	}()

	// Wait for server to start
	var serverAddr string
	select {
	case err := <-serverErrCh:
		t.Fatalf("Server failed to start: %v", err)
	case addr := <-serverAddrCh:
		serverAddr = addr
	case <-time.After(5 * time.Second):
		t.Fatal("Server startup timeout")
	}

	// 6. Send a test ingest request with dmesg lines
	delta := protocol.DmesgDelta{
		Hostname:  "integration-test-host",
		Timestamp: time.Now(),
		Lines: []string{
			"[Mon Feb 3 12:00:00 2026] EDAC MC0: 1 CE memory error",
			"[Mon Feb 3 12:00:01 2026] Normal kernel message",
		},
	}
	body, _ := json.Marshal(delta)

	// Create HTTP client that skips TLS verification (self-signed cert)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("POST", "https://"+serverAddr+"/ingest", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /ingest failed: %v", err)
	}
	defer resp.Body.Close()

	// 7. Verify response is 200 OK
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Parse response
	var ingestResp struct {
		Status    string `json:"status"`
		LatencyMs int64  `json:"latency_ms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ingestResp); err != nil {
		t.Fatalf("Decode response: %v", err)
	}

	if ingestResp.Status != "warning" {
		t.Errorf("Response status = %q, want %q", ingestResp.Status, "warning")
	}

	// 8. Verify result is stored in SQLite with expected values
	// Open the DB directly to verify storage
	db, err := collector.NewDB(dbPath)
	if err != nil {
		t.Fatalf("Open DB for verification: %v", err)
	}
	defer db.Close()

	results, err := db.QueryByHostname("integration-test-host", 10)
	if err != nil {
		t.Fatalf("QueryByHostname: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result in DB, got %d", len(results))
	}

	result := results[0]
	if result.Hostname != "integration-test-host" {
		t.Errorf("Stored hostname = %q, want %q", result.Hostname, "integration-test-host")
	}
	if result.Status != "warning" {
		t.Errorf("Stored status = %q, want %q", result.Status, "warning")
	}
	if len(result.Issues) != 1 {
		t.Errorf("Stored issues count = %d, want 1", len(result.Issues))
	} else {
		if result.Issues[0].Severity != "warning" {
			t.Errorf("Issue severity = %q, want %q", result.Issues[0].Severity, "warning")
		}
	}
	if result.RawDmesg == "" {
		t.Error("Stored raw_dmesg is empty")
	}
	// Latency can be 0 for very fast mock responses (sub-millisecond)
	if result.APILatencyMs < 0 {
		t.Errorf("Stored api_latency_ms = %d, want >= 0", result.APILatencyMs)
	}
}

// generateTestCert creates a self-signed TLS certificate for testing
func generateTestCert(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()

	// Generate private key
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Generate key: %v", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("Create certificate: %v", err)
	}

	// Write cert file
	certFile = filepath.Join(dir, "cert.pem")
	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatalf("Create cert file: %v", err)
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certOut.Close()

	// Write key file
	keyFile = filepath.Join(dir, "key.pem")
	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatalf("Create key file: %v", err)
	}
	privBytes, _ := x509.MarshalECPrivateKey(priv)
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})
	keyOut.Close()

	return certFile, keyFile
}
