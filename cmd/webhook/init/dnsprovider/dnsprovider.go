package dnsprovider

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/EfficientIP-Labs/external-dns-efficientip-webhook/cmd/webhook/init/configuration"
	"github.com/EfficientIP-Labs/external-dns-efficientip-webhook/internal/efficientip"
	"github.com/caarlos0/env/v11"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/provider"

	log "github.com/sirupsen/logrus"
)

// nolint: revive
func Init(config configuration.Config) (provider.Provider, error) {
	var domainFilter endpoint.DomainFilter
	createMsg := "Creating EfficientIP provider with "

	if config.RegexDomainFilter != "" {
		createMsg += fmt.Sprintf("regexp domain filter: '%s', ", config.RegexDomainFilter)
		if config.RegexDomainExclusion != "" {
			createMsg += fmt.Sprintf("with exclusion: '%s', ", config.RegexDomainExclusion)
		}
		domainFilter = endpoint.NewRegexDomainFilter(
			regexp.MustCompile(config.RegexDomainFilter),
			regexp.MustCompile(config.RegexDomainExclusion),
		)
	} else {
		if config.DomainFilter != nil && len(config.DomainFilter) > 0 {
			createMsg += fmt.Sprintf("domain filter: '%s', ", strings.Join(config.DomainFilter, ","))
		}
		if config.ExcludeDomains != nil && len(config.ExcludeDomains) > 0 {
			createMsg += fmt.Sprintf("exclude domain filter: '%s', ", strings.Join(config.ExcludeDomains, ","))
		}
		domainFilter = endpoint.NewDomainFilterWithExclusions(config.DomainFilter, config.ExcludeDomains)
	}

	createMsg = strings.TrimSuffix(createMsg, ", ")
	if strings.HasSuffix(createMsg, "with ") {
		createMsg += "no kind of domain filters"
	}
	log.Info(createMsg)

	eipConfig := efficientip.EfficientIPConfig{}
    if err := env.Parse(&eipConfig); err != nil {
        return nil, fmt.Errorf("reading configuration failed: %v", err)
    } else {
        if eipConfig.Token == "" || eipConfig.Secret == "" {
            if eipConfig.Username == "" || eipConfig.Password == "" {
                return nil, fmt.Errorf("missing authentication credentials. Login/password or access token/secret are required")
            }
        }
    }
	eipConfig.FQDNRegEx = config.RegexDomainFilter
	eipConfig.NameRegEx = config.RegexNameFilter

	return efficientip.NewEfficientIPProvider(&eipConfig, domainFilter)
}
