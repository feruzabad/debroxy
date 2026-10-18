// Package in holds the driving ports of the provider context: operations,
// not commands and queries (ddd-rules A-10, ADR 0003 rule 2), and the
// neutral types they exchange.
package in

// Application is everything the provider context can be asked to do.
type Application struct {
	Operations Operations
}

type Operations struct {
	ListProviders      ListProvidersOperation
	HosterCatalogue    HosterCatalogueOperation
	CreateRemoteItem   CreateRemoteItemOperation
	ListRemoteItems    ListRemoteItemsOperation
	DescribeRemoteItem DescribeRemoteItemOperation
	DeleteRemoteItem   DeleteRemoteItemOperation
	CheckCached        CheckCachedOperation
	TranslateWebhook   TranslateWebhookOperation
	ProbeCapabilities  ProbeCapabilitiesOperation
	OpenDeliveryStream OpenDeliveryStreamOperation
}
