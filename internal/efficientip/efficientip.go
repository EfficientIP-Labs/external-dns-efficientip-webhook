package efficientip

import (
	"context"
	"fmt"
	"crypto/tls"
	"net/http"
	"strconv"

	eip "github.com/efficientip-labs/solidserver-go-client/sdsclient"
	log "github.com/sirupsen/logrus"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	// provider specific key to track if PTR record was already created or not for A records
	providerSpecificEfficientipPtrRecord = "efficientip-ptr-record-exists"
)

type EfficientIPConfig struct {
	Host       string `env:"EIP_HOST,required" envDefault:"localhost"`
	Port       int    `env:"EIP_PORT,required" envDefault:"443"`
	Username   string `env:"EIP_WAPI_USER,required"`
	Password   string `env:"EIP_WAPI_PASSWORD,required"`
	DnsSmart   string `env:"EIP_SMART,required"`
	DnsView    string `env:"EIP_VIEW" envDefault:""`
	SSLVerify  bool   `env:"EIP_SSL_VERIFY" envDefault:"true"`
	DryRun     bool   `env:"EIP_DRY_RUN" envDefault:"false"`
	MaxResults int    `env:"EIP_MAX_RESULTS" envDefault:"1500"`
	CreatePTR  bool   `env:"EIP_CREATE_PTR" envDefault:"false"`
	DefaultTTL int    `env:"EIP_DEFAULT_TTL" envDefault:"300"`
	FQDNRegEx  string
	NameRegEx  string
}

type EfficientipClient interface {
	ZonesList(config *EfficientIPConfig) ([]*ZoneAuth, error)
	RecordAdd(rr *endpoint.Endpoint) error
	RecordDelete(rr *endpoint.Endpoint) error
	RecordList(Zone ZoneAuth) (endpoints []*endpoint.Endpoint, _ error)
}

func NewEfficientipAPI(ctx context.Context, config *eip.Configuration) EfficientIPAPI {
	return EfficientIPAPI{
		client:  eip.NewAPIClient(config),
		context: ctx,
	}
}

type EfficientIPAPI struct {
	client  *eip.APIClient
	context context.Context
}

type Provider struct {
	provider.BaseProvider
	client       EfficientipClient
	domainFilter endpoint.DomainFilter
	context      context.Context
	config       *EfficientIPConfig
}

type ZoneAuth struct {
	Name string
	Type string
	ID   string
}

func NewZoneAuth(zone eip.DnsZoneDataData) *ZoneAuth {
	return &ZoneAuth{
		Name: zone.GetZoneName(),
		Type: zone.GetZoneType(),
		ID:   zone.GetZoneId(),
	}
}

// Creates a new EfficientIP provider.
func NewEfficientIPProvider(config *EfficientIPConfig, domainFilter endpoint.DomainFilter) (*Provider, error) {
	clientConfig := eip.NewConfiguration()
	if !config.SSLVerify {
		customTransport := http.DefaultTransport.(*http.Transport).Clone()
		customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		clientConfig.HTTPClient = &http.Client{Transport: customTransport}
	}

	ctx := context.WithValue(context.Background(), eip.ContextBasicAuth, eip.BasicAuth{
		UserName: config.Username,
		Password: config.Password,
	})
	ctx = context.WithValue(ctx, eip.ContextServerVariables, map[string]string{
		"host": config.Host,
		"port": strconv.Itoa(config.Port),
	})
	client := NewEfficientipAPI(ctx, clientConfig)

	provider := &Provider{
		client:       &client,
		domainFilter: domainFilter,
		context:      ctx,
		config:		  config,
	}

	return provider, nil
}

func (p *Provider) Zones() ([]*ZoneAuth, error) {
	var result []*ZoneAuth

	zones, err := p.client.ZonesList(p.config)

	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		if !p.domainFilter.Match(zone.Name) {
			log.Debugf("Ignore zone [%s] by domainFilter", zone.Name)
			continue
		}
		result = append(result, zone)
	}
	return result, nil
}

// Records gets the current records.
func (p *Provider) Records(ctx context.Context) (endpoints []*endpoint.Endpoint, err error) {
	log.Debug("fetching records...")
	zones, err := p.Zones()
	if err != nil {
		return nil, fmt.Errorf("could not fetch zones: %w", err)
	}

	for _, zone := range zones {
		log.Debugf("fetch records from zone '%s'", zone.Name)

		records, err := p.client.RecordList(*zone)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, records...)
	}

	log.Debugf("fetched %d records from efficientip", len(endpoints))
	return endpoints, nil
}

// ApplyChanges applies the given changes.
func (p *Provider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	for _, change := range changes.Delete {
		err := p.DeleteChanges(ctx, change)
		if err != nil {
			return err
		}
	}
	for _, change := range changes.UpdateOld {
		err := p.DeleteChanges(ctx, change)
		if err != nil {
			return err
		}
	}
	for _, change := range changes.UpdateNew {
		err := p.CreateChanges(ctx, change)
		if err != nil {
			return err
		}
	}
	for _, change := range changes.Create {
		err := p.CreateChanges(ctx, change)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Provider) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	// Update user specified TTL (0 == disabled)
	for _, ep := range endpoints {
		if !ep.RecordTTL.IsConfigured() {
			ep.RecordTTL = endpoint.TTL(p.config.DefaultTTL)
		}
	}

	if !p.config.CreatePTR {
		return endpoints, nil
	}

	// for all A records, we want to create PTR records
	// so add provider specific property to track if the record was created or not
	for i := range endpoints {
		if endpoints[i].RecordType == endpoint.RecordTypeA {
			found := false
			for j := range endpoints[i].ProviderSpecific {
				if endpoints[i].ProviderSpecific[j].Name == providerSpecificEfficientipPtrRecord {
					endpoints[i].ProviderSpecific[j].Value = "true"
					found = true
				}
			}
			if !found {
				endpoints[i].WithProviderSpecific(providerSpecificEfficientipPtrRecord, "true")
			}
		}
	}

	return endpoints, nil
}

func (p *Provider) DeleteChanges(_ context.Context, changes *endpoint.Endpoint) error {
	if p.config.DryRun {
		for _, value := range changes.Targets {
			log.Infof("Would delete %s record named '%s' to '%s' for Efficientip",
				changes.RecordType,
				changes.DNSName,
				value,
			)
		}
		return nil
	}
	_ = p.client.RecordDelete(changes)
	return nil
}

func (p *Provider) CreateChanges(_ context.Context, changes *endpoint.Endpoint) error {
	if p.config.DryRun {
		for _, value := range changes.Targets {
			log.Infof("Would create %s record named '%s' to '%s' for Efficientip",
				changes.RecordType,
				changes.DNSName,
				value,
			)
		}
		return nil
	}
	_ = p.client.RecordAdd(changes)
	return nil
}

func (e *EfficientIPAPI) ZonesList(config *EfficientIPConfig) ([]*ZoneAuth, error) {
	where := fmt.Sprintf("server_name%%3D%%27%s%%27", config.DnsSmart)
	if config.DnsView != "" {
		where += fmt.Sprintf("+AND+view_name%%3D%%27%s5%27", config.DnsView)
	}
	zones, _, err := e.client.DnsApi.DnsZoneList(e.context).Where(where).Execute()

	if err.Error() != "" && (!zones.HasSuccess() || !zones.GetSuccess()) {
		return nil, err
	}

	var result []*ZoneAuth
	for _, zone := range zones.GetData() {
		result = append(result, NewZoneAuth(zone))
	}

	return result, nil
}

func (e *EfficientIPAPI) RecordList(zone ZoneAuth) (endpoints []*endpoint.Endpoint, _ error) {
	records, _, err := e.client.DnsApi.DnsRrList(e.context).Where("zone_id=" + zone.ID).Orderby("rr_full_name").Execute()
	if err.Error() != "" && (!records.HasSuccess() || !records.GetSuccess()) {
		log.Errorf("Failed to get RRs from zone [%s]", zone.Name)
		return nil, err
	}

	Host := make(map[string]*endpoint.Endpoint)
	for _, rr := range records.GetData() {
		ttl, _ := strconv.Atoi(rr.GetRrTtl())

		switch rr.GetRrType() {
			case "A":
				log.Debugf("Found A Record : %s -> %s", rr.GetRrFullName(), rr.GetRrAllValue())
				if h, found := Host[rr.GetRrFullName()+":"+rr.GetRrType()]; found {
					h.Targets = append(h.Targets, rr.GetRrAllValue())
				} else {
					Host[rr.GetRrFullName()+":"+rr.GetRrType()] = endpoint.NewEndpointWithTTL(rr.GetRrFullName(), endpoint.RecordTypeA, endpoint.TTL(ttl), rr.GetRrAllValue())
				}
			case "TXT":
				log.Debugf("Found TXT Record : %s -> %s", rr.GetRrFullName(), rr.GetRrAllValue())
				tmp := endpoint.NewEndpointWithTTL(rr.GetRrFullName(), endpoint.RecordTypeTXT, endpoint.TTL(ttl), rr.GetRrAllValue())
				endpoints = append(endpoints, tmp)
			case "CNAME":
				log.Debugf("Found CNAME Record : %s -> %s", rr.GetRrFullName(), rr.GetRrAllValue())
				endpoints = append(endpoints, endpoint.NewEndpointWithTTL(rr.GetRrFullName(), rr.GetRrType(), endpoint.TTL(ttl), rr.GetRrAllValue()))
		}
	}
	for _, rr := range Host {
		endpoints = append(endpoints, rr)
	}
	return endpoints, nil
}

func (e *EfficientIPAPI) RecordDelete(rr *endpoint.Endpoint) error {
	for _, value := range rr.Targets {
		log.Infof("Deleting %s record named '%s' to '%s' for Efficientip",
			rr.RecordType,
			rr.DNSName,
			value,
		)

		_, _, err := e.client.DnsApi.DnsRrDelete(e.context).RrName(rr.DNSName).RrType(rr.RecordType).RrValue1(value).Execute()
		if err.Error() != "" {
			log.Errorf("Deletion of the RR %v %v -> %v : failed!", rr.RecordType, rr.DNSName, value)
		}
	}
	return nil
}

func (e *EfficientIPAPI) RecordAdd(rr *endpoint.Endpoint) error {
	for _, value := range rr.Targets {
		log.Infof("Creating %s record named '%s' to '%s' for Efficientip",
			rr.RecordType,
			rr.DNSName,
			value,
		)

		ttl := int32(rr.RecordTTL)
		_, _, err := e.client.DnsApi.DnsRrAdd(e.context).DnsRrAddInput(eip.DnsRrAddInput{
			RrName:   &rr.DNSName,
			RrType:   &rr.RecordType,
			RrTtl:    &ttl,
			RrValue1: &value,
		}).Execute()

		if err.Error() != "" {
			log.Errorf("Creation of the RR %v %v  [%v]-> %v : failed!", rr.RecordType, rr.DNSName, ttl, value)
		}
	}
	return nil
}
