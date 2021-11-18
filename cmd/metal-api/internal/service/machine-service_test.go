package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emicklei/go-restful/v3"
	goipam "github.com/metal-stack/go-ipam"
	"github.com/metal-stack/metal-api/cmd/metal-api/internal/datastore"
	"github.com/metal-stack/metal-api/cmd/metal-api/internal/ipam"
	"github.com/metal-stack/metal-api/cmd/metal-api/internal/metal"
	v1 "github.com/metal-stack/metal-api/cmd/metal-api/internal/service/v1"
	"github.com/metal-stack/metal-api/cmd/metal-api/internal/testdata"
	"github.com/metal-stack/metal-lib/bus"
	"github.com/metal-stack/metal-lib/httperrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	r "gopkg.in/rethinkdb/rethinkdb-go.v6"
)

const (
	testEmail = "test@test.example"
)

type emptyPublisher struct {
	doPublish func(topic string, data interface{}) error
}

func (p *emptyPublisher) Publish(topic string, data interface{}) error {
	if p.doPublish != nil {
		return p.doPublish(topic, data)
	}
	return nil
}

func (p *emptyPublisher) CreateTopic(topic string) error {
	return nil
}

func (p *emptyPublisher) Stop() {}

func TestGetMachines(t *testing.T) {
	ds, mock := datastore.InitMockDB()
	testdata.InitMockDBData(mock)

	machineservice, err := NewMachine(ds, &emptyPublisher{}, bus.DirectEndpoints(), ipam.New(goipam.New()), nil, nil, nil, nil, 0)
	require.NoError(t, err)
	container := restful.NewContainer().Add(machineservice)
	req := httptest.NewRequest("GET", "/v1/machine", nil)
	container = injectViewer(container, req)
	w := httptest.NewRecorder()
	container.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, w.Body.String())
	var result []v1.MachineResponse
	err = json.NewDecoder(resp.Body).Decode(&result)

	require.Nil(t, err)
	require.Len(t, result, len(testdata.TestMachines))
	require.Equal(t, testdata.M1.ID, result[0].ID)
	require.Equal(t, testdata.M1.Allocation.Name, result[0].Allocation.Name)
	require.Equal(t, testdata.Sz1.Name, *result[0].Size.Name)
	require.Equal(t, testdata.Partition1.Name, *result[0].Partition.Name)
	require.Equal(t, testdata.M2.ID, result[1].ID)
}

func TestRegisterMachine(t *testing.T) {
	tests := []struct {
		name                 string
		uuid                 string
		partitionid          string
		numcores             int
		memory               int
		dbpartitions         []metal.Partition
		dbsizes              []metal.Size
		dbmachines           metal.Machines
		neighbormac1         metal.MacAddress
		neighbormac2         metal.MacAddress
		expectedStatus       int
		expectedErrorMessage string
		expectedSizeName     string
	}{
		{
			name:             "insert new",
			uuid:             "0",
			partitionid:      "0",
			dbpartitions:     []metal.Partition{testdata.Partition1},
			dbsizes:          []metal.Size{testdata.Sz1},
			neighbormac1:     testdata.Switch1.Nics[0].MacAddress,
			neighbormac2:     testdata.Switch2.Nics[0].MacAddress,
			numcores:         1,
			memory:           100,
			expectedStatus:   http.StatusCreated,
			expectedSizeName: testdata.Sz1.Name,
		},
		{
			name:             "insert existing",
			uuid:             "1",
			partitionid:      "1",
			dbpartitions:     []metal.Partition{testdata.Partition1},
			dbsizes:          []metal.Size{testdata.Sz1},
			neighbormac1:     testdata.Switch1.Nics[0].MacAddress,
			neighbormac2:     testdata.Switch2.Nics[0].MacAddress,
			dbmachines:       metal.Machines{testdata.M1},
			numcores:         1,
			memory:           100,
			expectedStatus:   http.StatusOK,
			expectedSizeName: testdata.Sz1.Name,
		},
		{
			name:                 "insert existing without second neighbor",
			uuid:                 "1",
			partitionid:          "1",
			dbpartitions:         []metal.Partition{testdata.Partition1},
			dbsizes:              []metal.Size{testdata.Sz1},
			neighbormac1:         testdata.Switch1.Nics[0].MacAddress,
			dbmachines:           metal.Machines{testdata.M1},
			numcores:             1,
			memory:               100,
			expectedStatus:       http.StatusUnprocessableEntity,
			expectedErrorMessage: "machine 1 is not connected to exactly two switches, found connections to 1 switches",
		},
		{
			name:                 "empty uuid",
			uuid:                 "",
			partitionid:          "1",
			dbpartitions:         []metal.Partition{testdata.Partition1},
			dbsizes:              []metal.Size{testdata.Sz1},
			expectedStatus:       http.StatusUnprocessableEntity,
			expectedErrorMessage: "uuid cannot be empty",
		},
		{
			name:                 "empty partition",
			uuid:                 "1",
			partitionid:          "",
			dbpartitions:         nil,
			dbsizes:              []metal.Size{testdata.Sz1},
			expectedStatus:       http.StatusNotFound,
			expectedErrorMessage: "no partition with id \"\" found",
		},
		{
			name:             "new with unknown size",
			uuid:             "0",
			partitionid:      "1",
			dbpartitions:     []metal.Partition{testdata.Partition1},
			dbsizes:          []metal.Size{testdata.Sz1},
			neighbormac1:     testdata.Switch1.Nics[0].MacAddress,
			neighbormac2:     testdata.Switch2.Nics[0].MacAddress,
			numcores:         2,
			memory:           100,
			expectedStatus:   http.StatusCreated,
			expectedSizeName: metal.UnknownSize.Name,
		},
	}

	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			ds, mock := datastore.InitMockDB()
			mock.On(r.DB("mockdb").Table("partition").Get(tt.partitionid)).Return(tt.dbpartitions, nil)

			if len(tt.dbmachines) > 0 {
				mock.On(r.DB("mockdb").Table("size").Get(tt.dbmachines[0].SizeID)).Return([]metal.Size{testdata.Sz1}, nil)
				mock.On(r.DB("mockdb").Table("machine").Get(tt.dbmachines[0].ID).Replace(r.MockAnything())).Return(testdata.EmptyResult, nil)
			} else {
				mock.On(r.DB("mockdb").Table("machine").Get("0")).Return(nil, nil)
				mock.On(r.DB("mockdb").Table("machine").Insert(r.MockAnything(), r.InsertOpts{
					Conflict: "replace",
				})).Return(testdata.EmptyResult, nil)
			}
			mock.On(r.DB("mockdb").Table("size").Get(metal.UnknownSize.ID)).Return([]metal.Size{*metal.UnknownSize}, nil)
			mock.On(r.DB("mockdb").Table("switch").Filter(r.MockAnything(), r.FilterOpts{})).Return([]metal.Switch{testdata.Switch1, testdata.Switch2}, nil)
			mock.On(r.DB("mockdb").Table("event").Filter(r.MockAnything(), r.FilterOpts{})).Return([]metal.ProvisioningEventContainer{}, nil)
			mock.On(r.DB("mockdb").Table("event").Insert(r.MockAnything(), r.InsertOpts{})).Return(testdata.EmptyResult, nil)
			testdata.InitMockDBData(mock)

			registerRequest := &v1.MachineRegisterRequest{
				UUID:        tt.uuid,
				PartitionID: tt.partitionid,
				RackID:      "1",
				IPMI: v1.MachineIPMI{
					Address:    testdata.IPMI1.Address,
					Interface:  testdata.IPMI1.Interface,
					MacAddress: testdata.IPMI1.MacAddress,
					Fru: v1.MachineFru{
						ChassisPartNumber:   &testdata.IPMI1.Fru.ChassisPartNumber,
						ChassisPartSerial:   &testdata.IPMI1.Fru.ChassisPartSerial,
						BoardMfg:            &testdata.IPMI1.Fru.BoardMfg,
						BoardMfgSerial:      &testdata.IPMI1.Fru.BoardMfgSerial,
						BoardPartNumber:     &testdata.IPMI1.Fru.BoardPartNumber,
						ProductManufacturer: &testdata.IPMI1.Fru.ProductManufacturer,
						ProductPartNumber:   &testdata.IPMI1.Fru.ProductPartNumber,
						ProductSerial:       &testdata.IPMI1.Fru.ProductSerial,
					},
				},
				Hardware: v1.MachineHardwareExtended{
					MachineHardwareBase: v1.MachineHardwareBase{
						CPUCores: tt.numcores,
						Memory:   uint64(tt.memory),
					},
					Nics: v1.MachineNicsExtended{
						v1.MachineNicExtended{
							Neighbors: v1.MachineNicsExtended{
								v1.MachineNicExtended{
									MachineNic: v1.MachineNic{
										MacAddress: string(tt.neighbormac1),
									},
								},
							},
						},
						v1.MachineNicExtended{
							Neighbors: v1.MachineNicsExtended{
								v1.MachineNicExtended{
									MachineNic: v1.MachineNic{
										MacAddress: string(tt.neighbormac2),
									},
								},
							},
						},
					},
				},
			}

			js, _ := json.Marshal(registerRequest)
			body := bytes.NewBuffer(js)
			machineservice, err := NewMachine(ds, &emptyPublisher{}, bus.DirectEndpoints(), ipam.New(goipam.New()), nil, nil, nil, nil, 0)
			require.NoError(t, err)
			container := restful.NewContainer().Add(machineservice)
			req := httptest.NewRequest("POST", "/v1/machine/register", body)
			req.Header.Add("Content-Type", "application/json")
			container = injectEditor(container, req)
			w := httptest.NewRecorder()
			container.ServeHTTP(w, req)

			resp := w.Result()
			defer resp.Body.Close()
			require.Equal(t, tt.expectedStatus, resp.StatusCode, w.Body.String())

			if tt.expectedStatus > 300 {
				var result httperrors.HTTPErrorResponse
				err := json.NewDecoder(resp.Body).Decode(&result)

				require.Nil(t, err)
				require.Regexp(t, tt.expectedErrorMessage, result.Message)
			} else {
				var result v1.MachineResponse
				err := json.NewDecoder(resp.Body).Decode(&result)

				require.Nil(t, err)
				expectedid := "0"
				if len(tt.dbmachines) > 0 {
					expectedid = tt.dbmachines[0].ID
				}
				require.Equal(t, expectedid, result.ID)
				require.Equal(t, "1", result.RackID)
				require.Equal(t, tt.expectedSizeName, *result.Size.Name)
				require.Equal(t, testdata.Partition1.Name, *result.Partition.Name)
			}
		})
	}
}

func TestMachineIPMIReport(t *testing.T) {
	ds, mock := datastore.InitMockDB()
	testdata.InitMockDBData(mock)

	tests := []struct {
		name           string
		input          v1.MachineIpmiReports
		output         v1.MachineIpmiReportResponse
		wantStatusCode int
	}{
		{
			name: "update machine1 ipmi address",
			input: v1.MachineIpmiReports{
				PartitionID: testdata.M1.PartitionID,
				Reports:     map[string]v1.MachineIpmiReport{testdata.M1.ID: {BMCIp: "192.167.0.1"}},
			},
			output: v1.MachineIpmiReportResponse{
				Updated: []string{testdata.M1.ID},
				Created: []string{},
			},
			wantStatusCode: http.StatusOK,
		},
		{
			name: "don't update machine with unkown mac",
			input: v1.MachineIpmiReports{
				PartitionID: testdata.M1.PartitionID,
				Reports:     map[string]v1.MachineIpmiReport{"xyz": {BMCIp: "192.167.0.1"}},
			},
			output: v1.MachineIpmiReportResponse{
				Updated: []string{},
				Created: []string{"xyz"},
			},
			wantStatusCode: http.StatusOK,
		},
	}

	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			machineservice, err := NewMachine(ds, &emptyPublisher{}, bus.DirectEndpoints(), ipam.New(goipam.New()), nil, nil, nil, nil, 0)
			require.NoError(t, err)
			container := restful.NewContainer().Add(machineservice)
			js, _ := json.Marshal(tt.input)
			body := bytes.NewBuffer(js)
			req := httptest.NewRequest("POST", "/v1/machine/ipmi", body)
			req.Header.Add("Content-Type", "application/json")
			container = injectEditor(container, req)
			w := httptest.NewRecorder()
			container.ServeHTTP(w, req)

			resp := w.Result()
			defer resp.Body.Close()
			require.Equal(t, tt.wantStatusCode, resp.StatusCode, w.Body.String())

			var result v1.MachineIpmiReportResponse
			err = json.NewDecoder(resp.Body).Decode(&result)
			require.Nil(t, err)
			require.Equal(t, tt.output, result)
		})
	}
}

func TestMachineFindIPMI(t *testing.T) {
	tests := []struct {
		name           string
		machine        *metal.Machine
		wantStatusCode int
	}{
		{
			name:           "retrieve machine1 ipmi",
			machine:        &testdata.M1,
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "retrieve machine2 ipmi",
			machine:        &testdata.M2,
			wantStatusCode: http.StatusOK,
		},
	}

	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			ds, mock := datastore.InitMockDB()
			mock.On(r.DB("mockdb").Table("machine").Filter(r.MockAnything())).Return([]interface{}{*tt.machine}, nil)
			testdata.InitMockDBData(mock)

			machineservice, err := NewMachine(ds, &emptyPublisher{}, bus.DirectEndpoints(), ipam.New(goipam.New()), nil, nil, nil, nil, 0)
			require.NoError(t, err)
			container := restful.NewContainer().Add(machineservice)

			query := datastore.MachineSearchQuery{
				ID: &tt.machine.ID,
			}
			js, _ := json.Marshal(query)
			body := bytes.NewBuffer(js)
			req := httptest.NewRequest("POST", "/v1/machine/ipmi/find", body)
			req.Header.Add("Content-Type", "application/json")
			container = injectViewer(container, req)
			w := httptest.NewRecorder()
			container.ServeHTTP(w, req)

			resp := w.Result()
			defer resp.Body.Close()
			require.Equal(t, tt.wantStatusCode, resp.StatusCode, w.Body.String())

			var results []*v1.MachineIPMIResponse
			err = json.NewDecoder(resp.Body).Decode(&results)

			require.Nil(t, err)
			require.Len(t, results, 1)

			result := results[0]

			require.Equal(t, tt.machine.IPMI.Address, result.IPMI.Address)
			require.Equal(t, tt.machine.IPMI.Interface, result.IPMI.Interface)
			require.Equal(t, tt.machine.IPMI.User, result.IPMI.User)
			require.Equal(t, tt.machine.IPMI.Password, result.IPMI.Password)
			require.Equal(t, tt.machine.IPMI.MacAddress, result.IPMI.MacAddress)

			require.Equal(t, tt.machine.IPMI.Fru.ChassisPartNumber, *result.IPMI.Fru.ChassisPartNumber)
			require.Equal(t, tt.machine.IPMI.Fru.ChassisPartSerial, *result.IPMI.Fru.ChassisPartSerial)
			require.Equal(t, tt.machine.IPMI.Fru.BoardMfg, *result.IPMI.Fru.BoardMfg)
			require.Equal(t, tt.machine.IPMI.Fru.BoardMfgSerial, *result.IPMI.Fru.BoardMfgSerial)
			require.Equal(t, tt.machine.IPMI.Fru.BoardPartNumber, *result.IPMI.Fru.BoardPartNumber)
			require.Equal(t, tt.machine.IPMI.Fru.ProductManufacturer, *result.IPMI.Fru.ProductManufacturer)
			require.Equal(t, tt.machine.IPMI.Fru.ProductPartNumber, *result.IPMI.Fru.ProductPartNumber)
			require.Equal(t, tt.machine.IPMI.Fru.ProductSerial, *result.IPMI.Fru.ProductSerial)
		})
	}
}

func TestFinalizeMachineAllocation(t *testing.T) {
	ds, mock := datastore.InitMockDB()
	testdata.InitMockDBData(mock)

	tests := []struct {
		name           string
		machineID      string
		wantStatusCode int
		wantErr        bool
		wantErrMessage string
	}{
		{
			name:           "finalize successfully",
			machineID:      "1",
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "finalize unknown machine",
			machineID:      "999",
			wantStatusCode: http.StatusNotFound,
			wantErr:        true,
		},
		{
			name:           "finalize unallocated machine",
			machineID:      "3",
			wantStatusCode: http.StatusUnprocessableEntity,
			wantErr:        true,
			wantErrMessage: "the machine \"3\" is not allocated",
		},
	}

	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			machineservice, err := NewMachine(ds, &emptyPublisher{}, bus.DirectEndpoints(), ipam.New(goipam.New()), nil, nil, nil, nil, 0)
			require.NoError(t, err)
			container := restful.NewContainer().Add(machineservice)

			finalizeRequest := v1.MachineFinalizeAllocationRequest{
				Kernel: "vmlinuz",
			}

			js, _ := json.Marshal(finalizeRequest)
			body := bytes.NewBuffer(js)
			req := httptest.NewRequest("POST", fmt.Sprintf("/v1/machine/%s/finalize-allocation", tt.machineID), body)
			req.Header.Add("Content-Type", "application/json")
			container = injectEditor(container, req)
			w := httptest.NewRecorder()
			container.ServeHTTP(w, req)

			resp := w.Result()
			defer resp.Body.Close()
			require.Equal(t, tt.wantStatusCode, resp.StatusCode, w.Body.String())

			if tt.wantErr {
				var result httperrors.HTTPErrorResponse
				err := json.NewDecoder(resp.Body).Decode(&result)

				require.Nil(t, err)
				require.Equal(t, tt.wantStatusCode, result.StatusCode)
				if tt.wantErrMessage != "" {
					require.Regexp(t, tt.wantErrMessage, result.Message)
				}
			} else {
				var result v1.MachineResponse
				err := json.NewDecoder(resp.Body).Decode(&result)

				require.Nil(t, err)
			}
		})
	}
}

func TestSetMachineState(t *testing.T) {
	ds, mock := datastore.InitMockDB()
	testdata.InitMockDBData(mock)

	machineservice, err := NewMachine(ds, &emptyPublisher{}, bus.DirectEndpoints(), ipam.New(goipam.New()), nil, nil, nil, nil, 0)
	require.NoError(t, err)

	container := restful.NewContainer().Add(machineservice)

	stateRequest := v1.MachineState{
		Value:       string(metal.ReservedState),
		Description: "blubber",
	}
	js, _ := json.Marshal(stateRequest)
	body := bytes.NewBuffer(js)
	req := httptest.NewRequest("POST", "/v1/machine/1/state", body)
	req.Header.Add("Content-Type", "application/json")
	container = injectEditor(container, req)
	w := httptest.NewRecorder()
	container.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, w.Body.String())
	var result v1.MachineResponse
	err = json.NewDecoder(resp.Body).Decode(&result)

	require.Nil(t, err)
	require.Equal(t, "1", result.ID)
	require.Equal(t, string(metal.ReservedState), result.State.Value)
	require.Equal(t, "blubber", result.State.Description)
}

func TestGetMachine(t *testing.T) {
	ds, mock := datastore.InitMockDB()
	testdata.InitMockDBData(mock)

	machineservice, err := NewMachine(ds, &emptyPublisher{}, bus.DirectEndpoints(), ipam.New(goipam.New()), nil, nil, nil, nil, 0)
	require.NoError(t, err)

	container := restful.NewContainer().Add(machineservice)
	req := httptest.NewRequest("GET", "/v1/machine/1", nil)
	container = injectViewer(container, req)
	w := httptest.NewRecorder()
	container.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, w.Body.String())
	var result v1.MachineResponse
	err = json.NewDecoder(resp.Body).Decode(&result)

	require.Nil(t, err)
	require.Equal(t, testdata.M1.ID, result.ID)
	require.Equal(t, testdata.M1.Allocation.Name, result.Allocation.Name)
	require.Equal(t, testdata.Sz1.Name, *result.Size.Name)
	require.Equal(t, testdata.Img1.Name, *result.Allocation.Image.Name)
	require.Equal(t, testdata.Partition1.Name, *result.Partition.Name)
}

func TestGetMachineNotFound(t *testing.T) {
	ds, mock := datastore.InitMockDB()
	testdata.InitMockDBData(mock)

	machineservice, err := NewMachine(ds, &emptyPublisher{}, bus.DirectEndpoints(), ipam.New(goipam.New()), nil, nil, nil, nil, 0)
	require.NoError(t, err)

	container := restful.NewContainer().Add(machineservice)
	req := httptest.NewRequest("GET", "/v1/machine/999", nil)
	container = injectEditor(container, req)
	w := httptest.NewRecorder()
	container.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode, w.Body.String())
}

func TestFreeMachine(t *testing.T) {
	// TODO: Add tests for IPAM, verifying that networks are cleaned up properly

	ds, mock := datastore.InitMockDB()
	testdata.InitMockDBData(mock)

	pub := &emptyPublisher{}
	events := []string{"1-machine", "1-machine", "releaseMachineNetworks", "1-switch"}
	eventidx := 0
	pub.doPublish = func(topic string, data interface{}) error {
		require.Equal(t, events[eventidx], topic)
		eventidx++
		if eventidx == 2 {
			dv := data.(metal.MachineEvent)
			require.Equal(t, "1", dv.OldMachineID)
		}
		return nil
	}

	machineservice, err := NewMachine(ds, pub, bus.NewEndpoints(nil, pub), ipam.New(goipam.New()), nil, nil, nil, nil, 0)
	require.NoError(t, err)

	container := restful.NewContainer().Add(machineservice)
	req := httptest.NewRequest("DELETE", "/v1/machine/1/free", nil)
	container = injectEditor(container, req)
	w := httptest.NewRecorder()
	container.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, w.Body.String())
	var result v1.MachineResponse
	err = json.NewDecoder(resp.Body).Decode(&result)

	require.Nil(t, err)
	require.Equal(t, testdata.M1.ID, result.ID)
	require.Nil(t, result.Allocation)
	require.Empty(t, result.Tags)
}

func TestSearchMachine(t *testing.T) {
	ds, mock := datastore.InitMockDB()
	mock.On(r.DB("mockdb").Table("machine").Filter(r.MockAnything())).Return([]interface{}{testdata.M1}, nil)
	testdata.InitMockDBData(mock)

	machineservice, err := NewMachine(ds, &emptyPublisher{}, bus.DirectEndpoints(), ipam.New(goipam.New()), nil, nil, nil, nil, 0)
	require.NoError(t, err)

	container := restful.NewContainer().Add(machineservice)
	requestJSON := fmt.Sprintf("{%q:[%q]}", "nics_mac_addresses", "1")
	req := httptest.NewRequest("POST", "/v1/machine/find", bytes.NewBufferString(requestJSON))
	req.Header.Add("Content-Type", "application/json")
	container = injectViewer(container, req)
	w := httptest.NewRecorder()
	container.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, w.Body.String())
	var results []v1.MachineResponse
	err = json.NewDecoder(resp.Body).Decode(&results)

	require.Nil(t, err)
	require.Len(t, results, 1)
	result := results[0]
	require.Equal(t, testdata.M1.ID, result.ID)
	require.Equal(t, testdata.M1.Allocation.Name, result.Allocation.Name)
	require.Equal(t, testdata.Sz1.Name, *result.Size.Name)
	require.Equal(t, testdata.Img1.Name, *result.Allocation.Image.Name)
	require.Equal(t, testdata.Partition1.Name, *result.Partition.Name)
}

func TestAddProvisioningEvent(t *testing.T) {
	ds, mock := datastore.InitMockDB()
	testdata.InitMockDBData(mock)

	machineservice, err := NewMachine(ds, &emptyPublisher{}, bus.DirectEndpoints(), ipam.New(goipam.New()), nil, nil, nil, nil, 0)
	require.NoError(t, err)

	container := restful.NewContainer().Add(machineservice)
	event := &metal.ProvisioningEvent{
		Event:   metal.ProvisioningEventPreparing,
		Message: "starting metal-hammer",
	}
	js, _ := json.Marshal(event)
	body := bytes.NewBuffer(js)
	req := httptest.NewRequest("POST", "/v1/machine/1/event", body)
	container = injectEditor(container, req)
	req.Header.Add("Content-Type", "application/json")
	w := httptest.NewRecorder()
	container.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, w.Body.String())
	var result v1.MachineRecentProvisioningEvents
	err = json.NewDecoder(resp.Body).Decode(&result)

	require.Nil(t, err)
	require.Equal(t, "0", result.IncompleteProvisioningCycles)
	require.Len(t, result.Events, 1)
	if len(result.Events) > 0 {
		require.Equal(t, "starting metal-hammer", result.Events[0].Message)
		require.Equal(t, string(metal.ProvisioningEventPreparing), result.Events[0].Event)
	}
}

func TestOnMachine(t *testing.T) {
	tests := []struct {
		cmd      metal.MachineCommand
		endpoint string
		param    string
	}{
		{
			cmd:      metal.MachineOnCmd,
			endpoint: "on",
		},
		{
			cmd:      metal.MachineOffCmd,
			endpoint: "off",
		},
		{
			cmd:      metal.MachineResetCmd,
			endpoint: "reset",
		},
		{
			cmd:      metal.MachineCycleCmd,
			endpoint: "cycle",
		},
		{
			cmd:      metal.MachineBiosCmd,
			endpoint: "bios",
		},
		{
			cmd:      metal.MachineDiskCmd,
			endpoint: "disk",
		},
		{
			cmd:      metal.MachinePxeCmd,
			endpoint: "pxe",
		},
		{
			cmd:      metal.ChassisIdentifyLEDOnCmd,
			endpoint: "chassis-identify-led-on",
		},
		{
			cmd:      metal.ChassisIdentifyLEDOnCmd,
			endpoint: "chassis-identify-led-on?description=test",
		},
		{
			cmd:      metal.ChassisIdentifyLEDOffCmd,
			endpoint: "chassis-identify-led-off?description=test",
		},
	}

	for i := range tests {
		tt := tests[i]
		t.Run("cmd_"+tt.endpoint, func(t *testing.T) {
			ds, mock := datastore.InitMockDB()
			testdata.InitMockDBData(mock)

			pub := &emptyPublisher{}
			pub.doPublish = func(topic string, data interface{}) error {
				require.Equal(t, "1-machine", topic)
				dv := data.(metal.MachineEvent)
				require.Equal(t, tt.cmd, dv.Cmd.Command)
				require.Equal(t, "1", dv.Cmd.TargetMachineID)
				return nil
			}

			machineservice, err := NewMachine(ds, pub, bus.DirectEndpoints(), ipam.New(goipam.New()), nil, nil, nil, nil, 0)
			require.NoError(t, err)

			js, _ := json.Marshal([]string{tt.param})
			body := bytes.NewBuffer(js)
			container := restful.NewContainer().Add(machineservice)
			req := httptest.NewRequest("POST", "/v1/machine/1/power/"+tt.endpoint, body)
			container = injectEditor(container, req)
			req.Header.Add("Content-Type", "application/json")
			w := httptest.NewRecorder()
			container.ServeHTTP(w, req)

			resp := w.Result()
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode, w.Body.String())
		})
	}
}

func TestParsePublicKey(t *testing.T) {
	pubKey := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDi4+MA0u/luzH2iaKnBTHzo+BEmV1MsdWtPtAps9ccD1vF94AqKtV6mm387ZhamfWUfD1b3Q5ftk56ekwZgHbk6PIUb/W4GrBD4uslTL2lzNX9v0Njo9DfapDKv4Tth6Qz5ldUb6z7IuyDmWqn3FbIPo4LOZxJ9z/HUWyau8+JMSpwIyzp2S0Gtm/pRXhbkZlr4h9jGApDQICPFGBWFEVpyOOjrS8JnEC8YzUszvbj5W1CH6Sn/DtxW0/CTAWwcjIAYYV8GlouWjjALqmjvpxO3F5kvQ1xR8IYrD86+cSCQSP4TpehftzaQzpY98fcog2YkEra+1GCY456cVSUhe1X"
	_, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubKey))
	require.Nil(t, err)

	pubKey = ""
	_, _, _, _, err = ssh.ParseAuthorizedKey([]byte(pubKey))
	require.NotNil(t, err)

	pubKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDi4+MA0u/luzH2iaKnBTHzo+BEmV1MsdWtPtAps9ccD1vF94AqKtV6mm387ZhamfWUfD1b3Q5ftk56ekwZgHbk6PIUb/W4GrBD4uslTL2lzNX9v0Njo9DfapDKv4Tth6Qz5ldUb6z7IuyDmWqn3FbIPo4LOZxJ9z/HUWyau8+JMSpwIyzp2S0Gtm/pRXhbkZlr4h9jGApDQICPFGBWFEVpyOOjrS8JnEC8YzUszvbj5W1CH6Sn/DtxW0/CTAWwcjIAYYV8GlouWjjALqmjvpxO3F5kvQ1xR8IYrD86+cSCQSP4TpehftzaQzpY98fcog2YkEra+1GCY456cVSUhe1"
	_, _, _, _, err = ssh.ParseAuthorizedKey([]byte(pubKey))
	require.NotNil(t, err)

	pubKey = "AAAAB3NzaC1yc2EAAAADAQABAAABAQDi4+MA0u/luzH2iaKnBTHzo+BEmV1MsdWtPtAps9ccD1vF94AqKtV6mm387ZhamfWUfD1b3Q5ftk56ekwZgHbk6PIUb/W4GrBD4uslTL2lzNX9v0Njo9DfapDKv4Tth6Qz5ldUb6z7IuyDmWqn3FbIPo4LOZxJ9z/HUWyau8+JMSpwIyzp2S0Gtm/pRXhbkZlr4h9jGApDQICPFGBWFEVpyOOjrS8JnEC8YzUszvbj5W1CH6Sn/DtxW0/CTAWwcjIAYYV8GlouWjjALqmjvpxO3F5kvQ1xR8IYrD86+cSCQSP4TpehftzaQzpY98fcog2YkEra+1GCY456cVSUhe1X"
	_, _, _, _, err = ssh.ParseAuthorizedKey([]byte(pubKey))
	require.NotNil(t, err)
}

func Test_validateAllocationSpec(t *testing.T) {
	ass := assert.New(t)
	trueValue := true
	falseValue := false

	tests := []struct {
		spec     machineAllocationSpec
		isError  bool
		name     string
		expected string
	}{
		{
			spec: machineAllocationSpec{
				UUID:      "gopher-uuid",
				Creator:   testEmail,
				ProjectID: "123",
				Role:      metal.RoleMachine,
				Networks: []v1.MachineAllocationNetwork{
					{
						NetworkID: "network",
					},
				},
				IPs: []string{"1.2.3.4"},
			},
			isError:  false,
			expected: "",
			name:     "auto acquire network and additional ip",
		},
		{
			spec: machineAllocationSpec{
				UUID:      "gopher-uuid",
				Creator:   testEmail,
				ProjectID: "123",
				Role:      metal.RoleMachine,
				Networks: []v1.MachineAllocationNetwork{
					{
						NetworkID:     "network",
						AutoAcquireIP: &trueValue,
					},
				},
			},
			isError: false,
			name:    "good case (explicit network)",
		},
		{
			spec: machineAllocationSpec{
				UUID:      "gopher-uuid",
				Creator:   testEmail,
				ProjectID: "123",
				Role:      metal.RoleMachine,
			},
			isError:  false,
			expected: "",
			name:     "good case (no network)",
		},
		{
			spec: machineAllocationSpec{
				Creator:     testEmail,
				PartitionID: "42",
				ProjectID:   "123",
				SizeID:      "42",
				Role:        metal.RoleMachine,
			},
			isError: false,
			name:    "partition and size id for absent uuid",
		},
		{
			spec: machineAllocationSpec{
				Creator:     testEmail,
				PartitionID: "42",
				ProjectID:   "123",
				Role:        metal.RoleMachine,
			},
			isError:  true,
			expected: "when no machine id is given, a size id must be specified",
			name:     "missing size id",
		},
		{
			spec: machineAllocationSpec{
				Creator:   testEmail,
				SizeID:    "42",
				ProjectID: "123",
				Role:      metal.RoleMachine,
			},
			isError:  true,
			expected: "when no machine id is given, a partition id must be specified",
			name:     "missing partition id",
		},
		{
			spec:     machineAllocationSpec{},
			isError:  true,
			expected: "project id must be specified",
			name:     "absent project id",
		},
		{
			spec: machineAllocationSpec{
				UUID:      "gopher-uuid",
				Creator:   testEmail,
				ProjectID: "123",
				Role:      metal.RoleMachine,
				Networks: []v1.MachineAllocationNetwork{
					{
						NetworkID:     "network",
						AutoAcquireIP: &falseValue,
					},
				},
			},
			isError:  true,
			expected: "missing ip(s) for network(s) without automatic ip allocation",
			name:     "missing ip definition for noauto network",
		},
		{
			spec: machineAllocationSpec{
				UUID:      "42",
				Creator:   testEmail,
				ProjectID: "123",
				IPs:       []string{"42"},
				Role:      metal.RoleMachine,
			},
			isError:  true,
			expected: `"42" is not a valid IP address`,
			name:     "illegal ip",
		},
		{
			spec: machineAllocationSpec{
				UUID:      "42",
				Creator:   testEmail,
				ProjectID: "123",
				Role:      metal.RoleFirewall,
			},
			isError:  true,
			expected: "when no ip is given at least one auto acquire network must be specified",
			name:     "missing network/ ip in case of firewall",
		},
		{
			spec: machineAllocationSpec{
				UUID:       "42",
				Creator:    testEmail,
				ProjectID:  "123",
				SSHPubKeys: []string{"42"},
				Role:       metal.RoleMachine,
			},
			isError:  true,
			expected: `invalid public SSH key: 42`,
			name:     "invalid ssh",
		},
		{
			spec: machineAllocationSpec{
				UUID:      "gopher-uuid",
				Creator:   testEmail,
				ProjectID: "123",
				Role:      metal.RoleMachine,
				Networks: []v1.MachineAllocationNetwork{
					{
						NetworkID: "network",
					},
				},
			},
			isError:  false,
			expected: "",
			name:     "implicit auto acquire network",
		},
	}

	for i := range tests {
		tt := tests[i]
		err := validateAllocationSpec(&tt.spec)
		if tt.isError {
			ass.Error(err, "Test: %s", tt.name)
			ass.EqualError(err, tt.expected, "Test: %s", tt.name)
		} else {
			ass.NoError(err, "Test: %s", tt.name)
		}
	}
}

func Test_makeMachineTags(t *testing.T) {
	type args struct {
		m        *metal.Machine
		userTags []string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "All possible tags",
			args: args{
				m: &metal.Machine{
					Allocation: &metal.MachineAllocation{
						MachineNetworks: []*metal.MachineNetwork{
							{
								Private: true,
								ASN:     1203874,
							},
						},
					},
					RackID: "rack01",
					IPMI: metal.IPMI{
						Fru: metal.Fru{
							ChassisPartSerial: "chassis123",
						},
					},
				},
				userTags: []string{"usertag=something"},
			},
			want: []string{
				"usertag=something",
				"machine.metal-stack.io/network.primary.asn=1203874",
				"machine.metal-stack.io/rack=rack01",
				"machine.metal-stack.io/chassis=chassis123",
			},
		},
		{
			name: "user tags higher precedence than network tags",
			args: args{
				m: &metal.Machine{
					Allocation: &metal.MachineAllocation{
						MachineNetworks: []*metal.MachineNetwork{},
					},
				},
				userTags: []string{"override=3"},
			},
			want: []string{
				"override=3",
			},
		},
		{
			name: "system tags higher precedence than user tags",
			args: args{
				m: &metal.Machine{
					Allocation: &metal.MachineAllocation{
						MachineNetworks: []*metal.MachineNetwork{
							{
								Private: true,
								ASN:     1203874,
							},
						},
					},
				},
				userTags: []string{"machine.metal-stack.io/network.primary.asn=iamdoingsomethingevil"},
			},
			want: []string{
				"machine.metal-stack.io/network.primary.asn=1203874",
			},
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			got := makeMachineTags(tt.args.m, tt.args.userTags)

			for _, wantElement := range tt.want {
				require.Contains(t, got, wantElement, "tag not contained in result")
			}
			require.Len(t, got, len(tt.want))
		})
	}
}

func Test_gatherNetworksFromSpec(t *testing.T) {
	boolTrue := true
	boolFalse := false
	partitionSuperNetworks := metal.Networks{testdata.Partition1PrivateSuperNetwork, testdata.Partition2PrivateSuperNetwork}

	type mock struct {
		term     r.Term
		response interface{}
		err      error
	}
	tests := []struct {
		name                   string
		allocationSpec         *machineAllocationSpec
		partition              *metal.Partition
		partitionSuperNetworks metal.Networks
		mocks                  []mock
		want                   allocationNetworkMap
		wantErr                bool
		errRegex               string
	}{
		{
			name: "no networks given",
			allocationSpec: &machineAllocationSpec{
				Networks: v1.MachineAllocationNetworks{},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			wantErr:                true,
			errRegex:               "no private network given",
		},
		{
			name: "private network given",
			allocationSpec: &machineAllocationSpec{
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID:     testdata.Partition1ExistingPrivateNetwork.ID,
						AutoAcquireIP: &boolTrue,
					},
				},
				ProjectID: testdata.Partition1ExistingPrivateNetwork.ProjectID,
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			wantErr:                false,
			want: allocationNetworkMap{
				testdata.Partition1ExistingPrivateNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingPrivateNetwork,
					ips:         []metal.IP{},
					auto:        true,
					networkType: metal.PrivatePrimaryUnshared,
				},
			},
		},
		{
			name: "private network given, but no auto acquisition and no ip provided",
			allocationSpec: &machineAllocationSpec{
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID:     testdata.Partition1ExistingPrivateNetwork.ID,
						AutoAcquireIP: &boolFalse,
					},
				},
				ProjectID: testdata.Partition1ExistingPrivateNetwork.ProjectID,
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			wantErr:                true,
			errRegex:               "the private network .* has no auto ip acquisition, but no suitable IPs were provided",
		},
		{
			name: "private network and internet network given",
			allocationSpec: &machineAllocationSpec{
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID:     testdata.Partition1ExistingPrivateNetwork.ID,
						AutoAcquireIP: &boolTrue,
					},
					v1.MachineAllocationNetwork{
						NetworkID:     testdata.Partition1InternetNetwork.ID,
						AutoAcquireIP: &boolTrue,
					},
				},
				ProjectID: testdata.Partition1ExistingPrivateNetwork.ProjectID,
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			wantErr:                false,
			want: allocationNetworkMap{
				testdata.Partition1ExistingPrivateNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingPrivateNetwork,
					ips:         []metal.IP{},
					auto:        true,
					networkType: metal.PrivatePrimaryUnshared,
				},
				testdata.Partition1InternetNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1InternetNetwork,
					ips:         []metal.IP{},
					auto:        true,
					networkType: metal.External,
				},
			},
		},
		{
			name: "ip which does not belong to any related network given",
			allocationSpec: &machineAllocationSpec{
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID:     testdata.Partition1ExistingPrivateNetwork.ID,
						AutoAcquireIP: &boolTrue,
					},
				},
				IPs:       []string{testdata.Partition2InternetIP.IPAddress},
				ProjectID: testdata.Partition1ExistingPrivateNetwork.ProjectID,
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			wantErr:                true,
			errRegex:               "given ip .* is not in any of the given networks",
		},
		{
			name: "private network and internet network with no auto acquired internet ip",
			allocationSpec: &machineAllocationSpec{
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID:     testdata.Partition1ExistingPrivateNetwork.ID,
						AutoAcquireIP: &boolTrue,
					},
					v1.MachineAllocationNetwork{
						NetworkID:     testdata.Partition1InternetNetwork.ID,
						AutoAcquireIP: &boolFalse,
					},
				},
				IPs:       []string{testdata.Partition1InternetIP.IPAddress},
				ProjectID: testdata.Partition1ExistingPrivateNetwork.ProjectID,
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			wantErr:                false,
			want: allocationNetworkMap{
				testdata.Partition1ExistingPrivateNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingPrivateNetwork,
					ips:         []metal.IP{},
					auto:        true,
					networkType: metal.PrivatePrimaryUnshared,
				},
				testdata.Partition1InternetNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1InternetNetwork,
					ips:         []metal.IP{testdata.Partition1InternetIP},
					auto:        false,
					networkType: metal.External,
				},
			},
		},
		{
			name: "private of other network given",
			allocationSpec: &machineAllocationSpec{
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID:     testdata.Partition1ExistingPrivateNetwork.ID,
						AutoAcquireIP: &boolTrue,
					},
				},
				ProjectID: "another-project",
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			wantErr:                true,
			errRegex:               "the given private network does not belong to the project, which is not allowed",
		},
		{
			name: "try to assign machine to private network of other partition",
			allocationSpec: &machineAllocationSpec{
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID:     testdata.Partition2ExistingPrivateNetwork.ID,
						AutoAcquireIP: &boolTrue,
					},
				},
				ProjectID: testdata.Partition2ExistingPrivateNetwork.ProjectID,
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			wantErr:                true,
			errRegex:               "private network .* must be located in the partition where the machine is going to be placed",
		},
		{
			name: "try to assign machine to super network",
			allocationSpec: &machineAllocationSpec{
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID:     testdata.Partition1PrivateSuperNetwork.ID,
						AutoAcquireIP: &boolTrue,
					},
				},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			wantErr:                true,
			errRegex:               "private super networks are not allowed to be set explicitly",
		},
		{
			name: "try to assign machine to underlay network",
			allocationSpec: &machineAllocationSpec{
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID:     testdata.Partition1UnderlayNetwork.ID,
						AutoAcquireIP: &boolTrue,
					},
				},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			wantErr:                true,
			errRegex:               "underlay networks are not allowed to be set explicitly",
		},
		{
			name: "add machine to a shared network as primary private network",
			allocationSpec: &machineAllocationSpec{
				Role:      metal.RoleMachine,
				ProjectID: testdata.Partition1ExistingSharedNetwork.ProjectID,
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingSharedNetwork.ID,
					},
				},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			want: allocationNetworkMap{
				testdata.Partition1ExistingSharedNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingSharedNetwork,
					ips:         []metal.IP{},
					auto:        true,
					networkType: metal.PrivatePrimaryShared,
				},
			},
			wantErr: false,
		},
		{
			name: "add machine with specific ip to a shared network as primary private network",
			allocationSpec: &machineAllocationSpec{
				Role:      metal.RoleMachine,
				ProjectID: testdata.Partition1ExistingSharedNetwork.ProjectID,
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingSharedNetwork.ID,
					},
				},
				IPs: []string{testdata.Partition1SpecificSharedIP.IPAddress},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			want: allocationNetworkMap{
				testdata.Partition1ExistingSharedNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingSharedNetwork,
					ips:         []metal.IP{testdata.Partition1SpecificSharedIP},
					auto:        false,
					networkType: metal.PrivatePrimaryShared,
				},
			},
			wantErr: false,
		},
		{
			name: "add machine with specific ip to a shared network as primary private network with ip auto acquisition implicitly disabled",
			allocationSpec: &machineAllocationSpec{
				Role:      metal.RoleMachine,
				ProjectID: testdata.Partition1ExistingSharedNetwork.ProjectID,
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						AutoAcquireIP: &boolTrue,
						NetworkID:     testdata.Partition1ExistingSharedNetwork.ID,
					},
				},
				IPs: []string{testdata.Partition1SpecificSharedIP.IPAddress},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			want: allocationNetworkMap{
				testdata.Partition1ExistingSharedNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingSharedNetwork,
					ips:         []metal.IP{testdata.Partition1SpecificSharedIP},
					auto:        false,
					networkType: metal.PrivatePrimaryShared,
				},
			},
			wantErr: false,
		},
		{
			name: "add firewall to a shared network as primary private network",
			allocationSpec: &machineAllocationSpec{
				Role:      metal.RoleFirewall,
				ProjectID: testdata.Partition1ExistingSharedNetwork.ProjectID,
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingSharedNetwork.ID,
					},
				},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			want: allocationNetworkMap{
				testdata.Partition1ExistingSharedNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingSharedNetwork,
					ips:         []metal.IP{},
					auto:        true,
					networkType: metal.PrivatePrimaryShared,
				},
			},
			wantErr: false,
		},
		{
			name: "add firewall with specific ip to a shared network as primary private network",
			allocationSpec: &machineAllocationSpec{
				Role:      metal.RoleFirewall,
				ProjectID: testdata.Partition1ExistingSharedNetwork.ProjectID,
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingSharedNetwork.ID,
					},
				},
				IPs: []string{testdata.Partition1SpecificSharedIP.IPAddress},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			want: allocationNetworkMap{
				testdata.Partition1ExistingSharedNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingSharedNetwork,
					ips:         []metal.IP{testdata.Partition1SpecificSharedIP},
					auto:        false,
					networkType: metal.PrivatePrimaryShared,
				},
			},
			wantErr: false,
		},
		{
			name: "add firewall to private network and shared network",
			allocationSpec: &machineAllocationSpec{
				Role:      metal.RoleFirewall,
				ProjectID: testdata.Partition1ExistingPrivateNetwork.ProjectID,
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingPrivateNetwork.ID,
					},
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingSharedNetwork.ID,
					},
				},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			want: allocationNetworkMap{
				testdata.Partition1ExistingPrivateNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingPrivateNetwork,
					ips:         []metal.IP{},
					auto:        true,
					networkType: metal.PrivatePrimaryUnshared,
				},
				testdata.Partition1ExistingSharedNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingSharedNetwork,
					ips:         []metal.IP{},
					auto:        true,
					networkType: metal.PrivateSecondaryShared,
				},
			},
			wantErr: false,
		},
		{
			name: "add firewall to private and shared network with specific ip",
			allocationSpec: &machineAllocationSpec{
				Role:      metal.RoleFirewall,
				ProjectID: testdata.Partition1ExistingPrivateNetwork.ProjectID,
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingPrivateNetwork.ID,
					},
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingSharedNetwork.ID,
					},
				},
				IPs: []string{testdata.Partition1SpecificSharedConsumerIP.IPAddress},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			want: allocationNetworkMap{
				testdata.Partition1ExistingPrivateNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingPrivateNetwork,
					ips:         []metal.IP{},
					auto:        true,
					networkType: metal.PrivatePrimaryUnshared,
				},
				testdata.Partition1ExistingSharedNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingSharedNetwork,
					ips:         []metal.IP{testdata.Partition1SpecificSharedConsumerIP},
					auto:        false,
					networkType: metal.PrivateSecondaryShared,
				},
			},
			wantErr: false,
		},
		{
			name: "try to add firewall to private and shared network with specific ip that belongs to an other project",
			allocationSpec: &machineAllocationSpec{
				Role:      metal.RoleFirewall,
				ProjectID: testdata.Partition1ExistingPrivateNetwork.ProjectID,
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingPrivateNetwork.ID,
					},
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingSharedNetwork.ID,
					},
				},
				IPs: []string{testdata.Partition1SpecificSharedIP.IPAddress},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			errRegex:               "given ip .* with project id .* does not belong to the project of this allocation: .*",
			wantErr:                true,
		},
		{
			name: "add firewall to multiple, private, shared networks",
			allocationSpec: &machineAllocationSpec{
				Role:      metal.RoleFirewall,
				ProjectID: testdata.Partition1ExistingPrivateNetwork.ProjectID,
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingPrivateNetwork.ID,
					},
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingSharedNetwork.ID,
					},
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingSharedNetwork2.ID,
					},
				},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			want: allocationNetworkMap{
				testdata.Partition1ExistingPrivateNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingPrivateNetwork,
					ips:         []metal.IP{},
					auto:        true,
					networkType: metal.PrivatePrimaryUnshared,
				},
				testdata.Partition1ExistingSharedNetwork.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingSharedNetwork,
					ips:         []metal.IP{},
					auto:        true,
					networkType: metal.PrivateSecondaryShared,
				},
				testdata.Partition1ExistingSharedNetwork2.ID: &allocationNetwork{
					network:     &testdata.Partition1ExistingSharedNetwork2,
					ips:         []metal.IP{},
					auto:        true,
					networkType: metal.PrivateSecondaryShared,
				},
			},
			wantErr: false,
		},
		{
			name: "try to add firewall to multiple, private, shared networks",
			allocationSpec: &machineAllocationSpec{
				Role:      metal.RoleFirewall,
				ProjectID: testdata.Partition1ExistingSharedNetwork.ProjectID,
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingSharedNetwork.ID,
					},
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingSharedNetwork2.ID,
					},
				},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			errRegex:               "firewalls are not allowed to be placed into multiple private, shared networks",
			wantErr:                true,
		},
		{
			name: "try to add machine to private network and shared network",
			allocationSpec: &machineAllocationSpec{
				Role:      metal.RoleMachine,
				ProjectID: testdata.Partition1ExistingPrivateNetwork.ProjectID,
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingPrivateNetwork.ID,
					},
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingSharedNetwork.ID,
					},
				},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			errRegex:               "machines are not allowed to be placed into multiple private networks",
			wantErr:                true,
		},
		{
			name: "try to add machine to multiple private networks which are not shared",
			allocationSpec: &machineAllocationSpec{
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1ExistingPrivateNetwork.ID,
					},
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition2ExistingPrivateNetwork.ID,
					},
				},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			wantErr:                true,
			errRegex:               "multiple private networks are specified but there must be only one primary private network that must not be shared",
		},
		{
			name: "try to add the same network a couple of times",
			allocationSpec: &machineAllocationSpec{
				Networks: v1.MachineAllocationNetworks{
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1InternetNetwork.ID,
					},
					v1.MachineAllocationNetwork{
						NetworkID: testdata.Partition1InternetNetwork.ID,
					},
				},
			},
			partition:              &testdata.Partition1,
			partitionSuperNetworks: partitionSuperNetworks,
			wantErr:                true,
			errRegex:               "given network ids are not unique",
		},
	}

	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			// init tests
			ds, mock := datastore.InitMockDB()
			for _, testMock := range test.mocks {
				mock.On(testMock.term).Return(testMock.response, testMock.err)
			}
			testdata.InitMockDBData(mock)

			// run
			got, err := gatherNetworksFromSpec(ds, test.allocationSpec, test.partition, test.partitionSuperNetworks)
			// verify
			if err != nil {
				if !test.wantErr {
					t.Errorf("gatherNetworksFromSpec() error = %v, wantErr %v", err, test.wantErr)
					return
				}
				if test.errRegex != "" {
					require.Regexp(t, test.errRegex, err)
				}
				return
			}

			require.Len(t, got, len(test.want), "number of gathered networks is incorrect")
			for wantNetworkID, wantNetwork := range test.want {
				require.Contains(t, got, wantNetworkID)
				gotNetwork := got[wantNetworkID]
				require.Equal(t, wantNetwork.networkType, gotNetwork.networkType)

				var gotIPs []string
				for _, gotIP := range gotNetwork.ips {
					gotIPs = append(gotIPs, gotIP.IPAddress)
				}

				for _, wantIP := range wantNetwork.ips {
					require.Contains(t, gotIPs, wantIP.IPAddress)
				}
			}
		})
	}
}
