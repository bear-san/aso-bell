package rpcauth

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// TLSFiles は mTLS 用の証明書ファイル群。3 つすべてが設定されたときだけ有効になる。
// Manager と Provider で同じ環境変数名を使うため、env タグをここに置く。
type TLSFiles struct {
	CertFile string `env:"ASOBELL_RPC_TLS_CERT"`
	KeyFile  string `env:"ASOBELL_RPC_TLS_KEY"`
	CAFile   string `env:"ASOBELL_RPC_TLS_CA"`
}

// Enabled は mTLS を有効にすべきかを返す。
func (f TLSFiles) Enabled() bool {
	return f.CertFile != "" || f.KeyFile != "" || f.CAFile != ""
}

// Validate は一部だけ設定された状態(平文で動いてしまう事故)を拒否する。
func (f TLSFiles) Validate() error {
	if !f.Enabled() {
		return nil
	}
	if f.CertFile == "" || f.KeyFile == "" || f.CAFile == "" {
		return errors.New("ASOBELL_RPC_TLS_CERT, ASOBELL_RPC_TLS_KEY and ASOBELL_RPC_TLS_CA must be set together")
	}
	return nil
}

// TransportCredentials はサーバー・クライアント共通の mTLS 資格情報を返す。
// 未設定なら平文(Compose の内部ネットワーク前提)。
func (f TLSFiles) TransportCredentials(serverName string) (credentials.TransportCredentials, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	if !f.Enabled() {
		return insecure.NewCredentials(), nil
	}
	cert, err := tls.LoadX509KeyPair(f.CertFile, f.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load key pair: %w", err)
	}
	caPEM, err := os.ReadFile(f.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse ca %s: no certificates found", f.CAFile)
	}
	return credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		RootCAs:      pool,
		ServerName:   serverName,
	}), nil
}
