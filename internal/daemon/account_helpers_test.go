package daemon

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func portOf(hostport string) string {
	_, p, _ := net.SplitHostPort(hostport)
	return p
}

// freePort is a loopback address with a port nothing holds, as host:port.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// selfSigned writes a certificate for shepherd.example.com and returns its files and a
// pool that trusts it.
func selfSigned(t *testing.T, dir string) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "shepherd.example.com"},
		DNSNames: []string{"shepherd.example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(k)
	certFile, keyFile = filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600)
	cert, _ := x509.ParseCertificate(der)
	pool = x509.NewCertPool()
	pool.AddCert(cert)
	return
}

// tlsClient dials addr whatever host the URL names, trusting only pool.
func tlsClient(pool *x509.CertPool, serverName, addr string) *http.Client {
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: serverName},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}}
}
