package converter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func (c *SurgeConverter) convertSnell(spec contracts.SubscriptionSpec) string {
	version := extInt(spec.Extensions, "version")
	if version == 0 {
		version = 5
	}
	parts := []string{
		fmt.Sprintf("%s = snell", c.nodeName(spec, "SNELL")),
		spec.Host,
		strconv.FormatUint(uint64(spec.Port), 10),
		fmt.Sprintf("psk=%s", spec.Password),
		fmt.Sprintf("version=%d", version),
	}
	// TODO(snell-obfs): once Surge snell supports obfs=http/tls on the line
	// format, emit spec.Extensions["obfs"] / ["obfs_host"] here. snellNodeToSpec
	// in pkg/proxy/core/subscription/uri_convert.go already projects both.
	return strings.Join(parts, ", ")
}
