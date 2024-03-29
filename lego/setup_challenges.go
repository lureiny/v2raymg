package lego

import (
	"net"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/challenge/tlsalpn01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns"
	"github.com/go-acme/lego/v4/providers/http/memcached"
	"github.com/go-acme/lego/v4/providers/http/webroot"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/urfave/cli/v2"
)

func setupChallenges(ctx *cli.Context, client *lego.Client) {
	if !ctx.Bool("http") && !ctx.Bool("tls") && !ctx.IsSet("dns") {
		logger.Debug("No challenge selected. You must specify at least one challenge: `--http`, `--tls`, `--dns`.")
	}

	if ctx.Bool("http") {
		err := client.Challenge.SetHTTP01Provider(setupHTTPProvider(ctx))
		if err != nil {
			logger.Error("%v", err)
		}
	}

	if ctx.Bool("tls") {
		err := client.Challenge.SetTLSALPN01Provider(setupTLSProvider(ctx))
		if err != nil {
			logger.Error("%v", err)
		}
	}

	if ctx.IsSet("dns") {
		setupDNS(ctx, client)
	}
}

func setupHTTPProvider(ctx *cli.Context) challenge.Provider {
	switch {
	case ctx.IsSet("http.webroot"):
		ps, err := webroot.NewHTTPProvider(ctx.String("http.webroot"))
		if err != nil {
			logger.Error("%v", err)
		}
		return ps
	case ctx.IsSet("http.memcached-host"):
		ps, err := memcached.NewMemcachedProvider(ctx.StringSlice("http.memcached-host"))
		if err != nil {
			logger.Error("%v", err)
		}
		return ps
	case ctx.IsSet("http.port"):
		iface := ctx.String("http.port")
		if !strings.Contains(iface, ":") {
			logger.Debug("The --http switch only accepts interface:port or :port for its argument.")
		}

		host, port, err := net.SplitHostPort(iface)
		if err != nil {
			logger.Error("%v", err)
		}

		srv := http01.NewProviderServer(host, port)
		if header := ctx.String("http.proxy-header"); header != "" {
			srv.SetProxyHeader(header)
		}
		return srv
	case ctx.Bool("http"):
		srv := http01.NewProviderServer("", "")
		if header := ctx.String("http.proxy-header"); header != "" {
			srv.SetProxyHeader(header)
		}
		return srv
	default:
		logger.Error("Invalid HTTP challenge options.")
		return nil
	}
}

func setupTLSProvider(ctx *cli.Context) challenge.Provider {
	switch {
	case ctx.IsSet("tls.port"):
		iface := ctx.String("tls.port")
		if !strings.Contains(iface, ":") {
			logger.Debug("The --tls switch only accepts interface:port or :port for its argument.")
		}

		host, port, err := net.SplitHostPort(iface)
		if err != nil {
			logger.Error("%v", err)
		}

		return tlsalpn01.NewProviderServer(host, port)
	case ctx.Bool("tls"):
		return tlsalpn01.NewProviderServer("", "")
	default:
		logger.Error("Invalid HTTP challenge options.")
		return nil
	}
}

func setupDNS(ctx *cli.Context, client *lego.Client) {
	provider, err := dns.NewDNSChallengeProviderByName(ctx.String("dns"))
	if err != nil {
		logger.Error("%v", err)
	}

	servers := ctx.StringSlice("dns.resolvers")
	err = client.Challenge.SetDNS01Provider(provider,
		dns01.CondOption(len(servers) > 0,
			dns01.AddRecursiveNameservers(dns01.ParseNameservers(ctx.StringSlice("dns.resolvers")))),
		dns01.CondOption(ctx.Bool("dns.disable-cp"),
			dns01.DisableCompletePropagationRequirement()),
		dns01.CondOption(ctx.IsSet("dns-timeout"),
			dns01.AddDNSTimeout(time.Duration(ctx.Int("dns-timeout"))*time.Second)),
	)
	if err != nil {
		logger.Error("%v", err)
	}
}
