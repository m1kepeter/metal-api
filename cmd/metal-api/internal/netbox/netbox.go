package netbox

import (
	"fmt"

	"git.f-i-ts.de/cloud-native/metal/metal-api/cmd/metal-api/internal/metal"
	"git.f-i-ts.de/cloud-native/metal/metal-api/netbox-api/client"
	nbdevice "git.f-i-ts.de/cloud-native/metal/metal-api/netbox-api/client/devices"
	nbswitch "git.f-i-ts.de/cloud-native/metal/metal-api/netbox-api/client/switches"
	"git.f-i-ts.de/cloud-native/metal/metal-api/netbox-api/models"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
	"github.com/spf13/viper"
)

// An APIProxy can be used to call netbox api functions. It wraps the token
// and has fields for the different functions. One can override the functions
// if this is needed (for example in testing code).
type APIProxy struct {
	*client.NetboxAPIProxy
	apitoken         string
	DoRegister       func(params *nbdevice.NetboxAPIProxyAPIDeviceRegisterParams, authInfo runtime.ClientAuthInfoWriter) (*nbdevice.NetboxAPIProxyAPIDeviceRegisterOK, error)
	DoAllocate       func(params *nbdevice.NetboxAPIProxyAPIDeviceAllocateParams, authInfo runtime.ClientAuthInfoWriter) (*nbdevice.NetboxAPIProxyAPIDeviceAllocateOK, error)
	DoRelease        func(params *nbdevice.NetboxAPIProxyAPIDeviceReleaseParams, authInfo runtime.ClientAuthInfoWriter) (*nbdevice.NetboxAPIProxyAPIDeviceReleaseOK, error)
	DoRegisterSwitch func(params *nbswitch.NetboxAPIProxyAPISwitchRegisterParams, authInfo runtime.ClientAuthInfoWriter) (*nbswitch.NetboxAPIProxyAPISwitchRegisterOK, error)
}

// New creates a new API proxy and uses the token from the viper-environment "netbox-api-token".
func New() *APIProxy {
	apitoken := viper.GetString("netbox-api-token")
	proxy := initNetboxProxy()
	return &APIProxy{
		NetboxAPIProxy:   proxy,
		apitoken:         apitoken,
		DoRegister:       proxy.Devices.NetboxAPIProxyAPIDeviceRegister,
		DoAllocate:       proxy.Devices.NetboxAPIProxyAPIDeviceAllocate,
		DoRelease:        proxy.Devices.NetboxAPIProxyAPIDeviceRelease,
		DoRegisterSwitch: proxy.Switches.NetboxAPIProxyAPISwitchRegister,
	}
}

func initNetboxProxy() *client.NetboxAPIProxy {
	netboxAddr := viper.GetString("netbox-addr")
	cfg := client.DefaultTransportConfig().WithHost(netboxAddr)
	return client.NewHTTPClientWithConfig(strfmt.Default, cfg)
}

func transformNicList(hwnics []metal.Nic) []*models.Nic {
	var nics []*models.Nic
	for i := range hwnics {
		nic := hwnics[i]
		m := string(nic.MacAddress)
		newnic := new(models.Nic)
		newnic.Mac = &m
		newnic.Name = &nic.Name
		nics = append(nics, newnic)
	}
	return nics
}

func (nb *APIProxy) authenticate(rq runtime.ClientRequest, rg strfmt.Registry) error {
	auth := "Token " + nb.apitoken
	rq.SetHeaderParam("Authorization", auth)
	return nil
}

// Register registers the given device in netbox.
func (nb *APIProxy) Register(siteid, rackid, size, uuid string, hwnics []metal.Nic) error {
	parms := nbdevice.NewNetboxAPIProxyAPIDeviceRegisterParams()
	parms.UUID = uuid
	nics := transformNicList(hwnics)
	parms.Request = &models.DeviceRegistrationRequest{
		Rack: &rackid,
		Site: &siteid,
		Size: &size,
		Nics: nics,
	}

	_, err := nb.DoRegister(parms, runtime.ClientAuthInfoWriterFunc(nb.authenticate))
	if err != nil {
		return fmt.Errorf("error calling netbox: %v", err)
	}
	return nil
}

// Allocate uses the given devices for the given tenant. On success it returns
// the CIDR which must be used in the new machine.
func (nb *APIProxy) Allocate(uuid string, tenant string, vrf uint, project, name, description, os string) (string, string, error) {
	parms := nbdevice.NewNetboxAPIProxyAPIDeviceAllocateParams()
	parms.UUID = uuid
	parms.Request = &models.DeviceAllocationRequest{
		Name:        &name,
		Tenant:      &tenant,
		Vrf:         fmt.Sprintf("%d", vrf),
		Project:     &project,
		Description: description,
		Os:          os,
	}

	rsp, err := nb.DoAllocate(parms, runtime.ClientAuthInfoWriterFunc(nb.authenticate))
	if err != nil {
		return "", "", fmt.Errorf("error calling netbox: %v", err)
	}
	return *rsp.Payload.Cidr, *rsp.Payload.VrfRd, nil
}

// Release releases the device with the given uuid in the netbox.
func (nb *APIProxy) Release(uuid string) error {
	parms := nbdevice.NewNetboxAPIProxyAPIDeviceReleaseParams()
	parms.UUID = uuid

	_, err := nb.DoRelease(parms, runtime.ClientAuthInfoWriterFunc(nb.authenticate))
	if err != nil {
		return fmt.Errorf("error calling netbox: %v", err)
	}
	return nil
}

// RegisterSwitch registers the switch on the netbox side.
func (nb *APIProxy) RegisterSwitch(siteid, rackid, uuid, name string, hwnics []metal.Nic) error {
	parms := nbswitch.NewNetboxAPIProxyAPISwitchRegisterParams()
	parms.UUID = uuid
	nics := transformNicList(hwnics)
	parms.Request = &models.SwitchRegistrationRequest{
		Name: &name,
		Rack: &rackid,
		Site: &siteid,
		Nics: nics,
	}

	_, err := nb.DoRegisterSwitch(parms, runtime.ClientAuthInfoWriterFunc(nb.authenticate))
	if err != nil {
		return fmt.Errorf("error calling netbox: %v", err)
	}
	return nil
}
