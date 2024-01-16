package telephony_providers_edges_phonebasesettings

import (
	"context"

	"github.com/mypurecloud/platform-client-sdk-go/v119/platformclientv2"
)

var internalProxy *phoneBaseProxy

type getPhoneBaseSettingFunc func(ctx context.Context, p *phoneBaseProxy, phoneBaseSettingsId string) (*platformclientv2.Phonebase, *platformclientv2.APIResponse, error)
type deletePhoneBaseSettingFunc func(ctx context.Context, p *phoneBaseProxy, phoneBaseSettingsId string) (*platformclientv2.APIResponse, error)
type putPhoneBaseSettingFunc func(ctx context.Context, p *phoneBaseProxy, phoneBaseSettingsId string, body platformclientv2.Phonebase) (*platformclientv2.Phonebase, *platformclientv2.APIResponse, error)
type postPhoneBaseSettingFunc func(ctx context.Context, p *phoneBaseProxy, body platformclientv2.Phonebase) (*platformclientv2.Phonebase, *platformclientv2.APIResponse, error)

// phoneBaseProxy contains all of the methods that call genesys cloud APIs.
type phoneBaseProxy struct {
	clientConfig *platformclientv2.Configuration
	edgesApi     *platformclientv2.TelephonyProvidersEdgeApi

	getPhoneBaseSettingAttr    getPhoneBaseSettingFunc
	deletePhoneBaseSettingAttr deletePhoneBaseSettingFunc
	putPhoneBaseSettingAttr    putPhoneBaseSettingFunc
	postPhoneBaseSettingAttr   postPhoneBaseSettingFunc
}

// newPhoneBaseProxy initializes the Phone Base proxy with all of the data needed to communicate with Genesys Cloud
func newphoneBaseProxy(clientConfig *platformclientv2.Configuration) *phoneBaseProxy {
	edgesApi := platformclientv2.NewTelephonyProvidersEdgeApiWithConfig(clientConfig)

	return &phoneBaseProxy{
		clientConfig: clientConfig,
		edgesApi:     edgesApi,

		getPhoneBaseSettingAttr:    getPhoneBaseSettingFn,
		deletePhoneBaseSettingAttr: deletePhoneBaseSettingsFn,
		putPhoneBaseSettingAttr:    putPhoneBaseSettingFn,
		postPhoneBaseSettingAttr:   postPhoneBaseSettingFn,
	}
}

// getPhoneProxy acts as a singleton to for the internalProxy.  It also ensures
// that we can still proxy our tests by directly setting internalProxy package variable
func getPhoneBaseProxy(clientConfig *platformclientv2.Configuration) *phoneBaseProxy {
	if internalProxy == nil {
		internalProxy = newphoneBaseProxy(clientConfig)
	}

	return internalProxy
}

func (p *phoneBaseProxy) getPhoneBaseSetting(ctx context.Context, phoneBaseSettingsId string) (*platformclientv2.Phonebase, *platformclientv2.APIResponse, error) {
	return p.getPhoneBaseSettingAttr(ctx, p, phoneBaseSettingsId)
}

func (p *phoneBaseProxy) deletePhoneBaseSetting(ctx context.Context, phoneBaseSettingsId string) (*platformclientv2.APIResponse, error) {
	return p.deletePhoneBaseSettingAttr(ctx, p, phoneBaseSettingsId)
}

func (p *phoneBaseProxy) putPhoneBaseSetting(ctx context.Context, phoneBaseSettingsId string, body platformclientv2.Phonebase) (*platformclientv2.Phonebase, *platformclientv2.APIResponse, error) {
	return p.putPhoneBaseSettingAttr(ctx, p, phoneBaseSettingsId, body)
}

func (p *phoneBaseProxy) postPhoneBaseSetting(ctx context.Context, body platformclientv2.Phonebase) (*platformclientv2.Phonebase, *platformclientv2.APIResponse, error) {
	return p.postPhoneBaseSettingAttr(ctx, p, body)
}

// getPhoneBaseSettingFn is an implementation function for retrieving a Genesys Cloud Phone Base Setting
func getPhoneBaseSettingFn(ctx context.Context, p *phoneBaseProxy, phoneBaseSettingsId string) (*platformclientv2.Phonebase, *platformclientv2.APIResponse, error) {
	phoneBase, resp, err := p.edgesApi.GetTelephonyProvidersEdgesPhonebasesetting(phoneBaseSettingsId)
	if err != nil {
		return nil, resp, err
	}

	return phoneBase, resp, nil
}

func deletePhoneBaseSettingsFn(ctx context.Context, p *phoneBaseProxy, phoneBaseSettingsId string) (*platformclientv2.APIResponse, error) {
	resp, err := p.edgesApi.DeleteTelephonyProvidersEdgesPhonebasesetting(phoneBaseSettingsId)
	return resp, err
}

func putPhoneBaseSettingFn(ctx context.Context, p *phoneBaseProxy, phoneBaseSettingsId string, body platformclientv2.Phonebase) (*platformclientv2.Phonebase, *platformclientv2.APIResponse, error) {
	phoneBase, resp, err := p.edgesApi.PutTelephonyProvidersEdgesPhonebasesetting(phoneBaseSettingsId, body)
	if err != nil {
		return nil, resp, err
	}

	return phoneBase, resp, nil
}

func postPhoneBaseSettingFn(ctx context.Context, p *phoneBaseProxy, body platformclientv2.Phonebase) (*platformclientv2.Phonebase, *platformclientv2.APIResponse, error) {
	phoneBase, resp, err := p.edgesApi.PostTelephonyProvidersEdgesPhonebasesettings(body)
	if err != nil {
		return nil, resp, err
	}

	return phoneBase, resp, nil
}
