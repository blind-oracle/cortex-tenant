package main

import (
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	fh "github.com/valyala/fasthttp"
	fhu "github.com/valyala/fasthttp/fasthttputil"
)

// Gets a random UUID for testing.
//
// Fails the test immediately if the UUID fails to generate.
func getUUID(t *testing.T) uuid.UUID {
	id, err := uuid.NewRandom()
	require.NoError(t, err)

	return id
}

// Gets a net.Addr for testing.
func getClientIP() net.Addr {
	return &net.IPAddr{
		IP: net.IPv4(10, 10, 10, 1),
	}
}

// An empty body callback function
//
// Used to allow us to call send() even though we do not care about the body
// contents at all.
func emptyBodyFunc() ([]byte, error) {
	return nil, nil
}

// Runs a config test
//
// cfgSetup is an optional callback to allow calling tests to configure the
// config struct to their needs.
//
// handler is the mock callback handler for the upstream server. Most assertions
// are expected to happen in this.
func runConfigTest(t *testing.T, cfgSetup func(*config), handler fh.RequestHandler) {
	cfg := config{}
	cfg.Tenant.Header = "X-Scope-OrgID"
	cfg.Timeout = 10 * time.Second
	cfg.pipeOut = fhu.NewInmemoryListener()

	if cfgSetup != nil {
		cfgSetup(&cfg)
	}

	p, err := newProcessor(cfg)
	require.NoError(t, err)

	s := &fh.Server{
		Handler: func(ctx *fh.RequestCtx) {
			handler(ctx)

			// Always return something to ensure p.send doesn't timeout
			ctx.WriteString("ok")
		},
	}

	go s.Serve(cfg.pipeOut)

	result := p.send("http://test/push", getClientIP(), getUUID(t), "", emptyBodyFunc)
	require.NoError(t, result.err)
}

// Tests that when username is not set, no auth header is sent
func Test_NoAuthHeader(t *testing.T) {
	runConfigTest(
		t,
		func(cfg *config) {
			// Not strictly needed as this is the default, but be explict for this
			// test that the username needs to be blank.
			cfg.Auth.Egress.Username = ""
		},
		func(ctx *fh.RequestCtx) {
			auth := ctx.Request.Header.Peek("Authorization")
			assert.Nil(t, auth, "No Authorization Header should have been set")
		},
	)
}

// Tests that when a username and password are set, an auth header containing these
// is sent
func Test_AuthHeader(t *testing.T) {
	username := "foo"
	password := "bar"

	runConfigTest(
		t,
		func(cfg *config) {
			cfg.Auth.Egress.Username = username
			cfg.Auth.Egress.Password = password
		},
		func(ctx *fh.RequestCtx) {
			auth := ctx.Request.Header.Peek("Authorization")
			if !assert.NotNil(t, auth, "Authorization Header was not set") {
				return
			}

			authContent, isBasic := strings.CutPrefix(string(auth), "Basic ")
			if !assert.True(t, isBasic, "Authorization Header was not of Basic type") {
				return
			}

			decodedContent, err := base64.StdEncoding.DecodeString(strings.Trim(authContent, " "))
			if !assert.NoError(t, err, "Authorization Header did not contain valid base64") {
				return
			}

			user, pass, isUserPassPair := strings.Cut(string(decodedContent), ":")
			if !assert.True(t, isUserPassPair, "Authorization Header did not container username:password pair") {
				return
			}

			assert.Equal(t, username, user, "Authorization Header Username is not correct")
			assert.Equal(t, password, pass, "Authorization Header Password is not correct")
		},
	)
}
