package datastore

import (
	v1 "git.f-i-ts.de/cloud-native/metal/metal-api/cmd/metal-api/internal/service/v1"
	"reflect"
	"testing"
	"testing/quick"

	"git.f-i-ts.de/cloud-native/metal/metal-api/cmd/metal-api/internal/metal"
	"git.f-i-ts.de/cloud-native/metal/metal-api/cmd/metal-api/internal/testdata"
	r "gopkg.in/rethinkdb/rethinkdb-go.v5"
)

// Test that generates many input data
// Reference: https://golang.org/pkg/testing/quick/
func TestRethinkStore_FindMachine2(t *testing.T) {

	// Mock the DB:
	ds, mock := InitMockDB()
	testdata.InitMockDBData(mock)

	f := func(x string) bool {
		_, err := ds.FindMachine(x)
		returnvalue := true
		if err != nil {
			returnvalue = false
		}
		return returnvalue
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}
func TestRethinkStore_FindMachine(t *testing.T) {

	// Mock the DB:
	ds, mock := InitMockDB()
	testdata.InitMockDBData(mock)

	type args struct {
		id string
	}
	tests := []struct {
		name    string
		rs      *RethinkStore
		args    args
		want    *metal.Machine
		wantErr bool
	}{
		// Test Data Array:
		{
			name: "Test 1",
			rs:   ds,
			args: args{
				id: "1",
			},
			want:    &testdata.M1,
			wantErr: false,
		},
		{
			name: "Test 2",
			rs:   ds,
			args: args{
				id: "2",
			},
			want:    &testdata.M2,
			wantErr: false,
		},
		{
			name: "Test 3",
			rs:   ds,
			args: args{
				id: "404",
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "Test 4",
			rs:   ds,
			args: args{
				id: "999",
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.rs.FindMachine(tt.args.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("RethinkStore.FindMachine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil {
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("RethinkStore.FindMachine() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestRethinkStore_SearchMachine(t *testing.T) {

	// Mock the DB:
	ds, mock := InitMockDB()
	testdata.InitMockDBData(mock)

	mock.On(r.DB("mockdb").Table("machine").Filter(func(m r.Term) r.Term {
		return m.Field("hardware").Field("network_interfaces").Map(func(nic r.Term) r.Term {
			return nic.Field("macAddress")
		}).Contains(r.Expr("11:11:11"))
	})).Return([]metal.Machine{
		testdata.M1,
	}, nil)

	type args struct {
		mac string
	}
	tests := []struct {
		name    string
		rs      *RethinkStore
		args    args
		want    []metal.Machine
		wantErr bool
	}{
		// Test Data Array:
		{
			name: "Test 1",
			rs:   ds,
			args: args{
				mac: "11:11:11",
			},
			want: []metal.Machine{
				testdata.M1,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &v1.FindMachinesRequest{
				NicsMacAddresses: []string{tt.args.mac},
			}
			got, err := tt.rs.FindMachines(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RethinkStore.FindMachines() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RethinkStore.FindMachines() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRethinkStore_SearchMachine2(t *testing.T) {

	// Mock the DB:
	ds, mock := InitMockDB()
	testdata.InitMockDBData(mock)

	mock.On(r.DB("mockdb").Table("machine").Filter(func(m r.Term) r.Term {
		return m.Field("block_devices").Map(func(bd r.Term) r.Term {
			return bd.Field("size")
		}).Contains(r.Expr(int64(1000000000000)))
	})).Return([]metal.Machine{
		testdata.M1,
	}, nil)

	type args struct {
		size int64
	}
	tests := []struct {
		name    string
		rs      *RethinkStore
		args    args
		want    []metal.Machine
		wantErr bool
	}{
		// Test Data Array:
		{
			name: "Test 1",
			rs:   ds,
			args: args{
				size: 1000000000000,
			},
			want: []metal.Machine{
				testdata.M1,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &v1.FindMachinesRequest{
				DiskSizes: []int64{tt.args.size},
			}
			got, err := tt.rs.FindMachines(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RethinkStore.FindMachines() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RethinkStore.FindMachines() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRethinkStore_SearchMachine3(t *testing.T) {

	// Mock the DB:
	ds, mock := InitMockDB()
	testdata.InitMockDBData(mock)

	mock.On(r.DB("mockdb").Table("machine").Filter(func(m r.Term) r.Term {
		return m.Field("allocation").Field("networks").Map(func(nw r.Term) r.Term {
			return nw.Field("networkid")
		}).Contains(r.Expr("1"))
	})).Return([]metal.Machine{
		testdata.M1,
	}, nil)

	type args struct {
		networkID string
	}
	tests := []struct {
		name    string
		rs      *RethinkStore
		args    args
		want    []metal.Machine
		wantErr bool
	}{
		// Test Data Array:
		{
			name: "Test 1",
			rs:   ds,
			args: args{
				networkID: "1",
			},
			want: []metal.Machine{
				testdata.M1,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &v1.FindMachinesRequest{
				NetworkIDs: []string{tt.args.networkID},
			}
			got, err := tt.rs.FindMachines(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RethinkStore.FindMachines() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RethinkStore.FindMachines() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRethinkStore_SearchMachine4(t *testing.T) {

	// Mock the DB:
	ds, mock := InitMockDB()
	testdata.InitMockDBData(mock)

	mock.On(r.DB("mockdb").Table("machine").Filter(func(m r.Term) r.Term {
		return m.Field("allocation").Field("networks").Map(func(nw r.Term) r.Term {
			return nw.Field("ips")
		}).Contains(r.Expr("1.2.3.4"))
	})).Return([]metal.Machine{
		testdata.M1,
	}, nil)

	type args struct {
		ip string
	}
	tests := []struct {
		name    string
		rs      *RethinkStore
		args    args
		want    []metal.Machine
		wantErr bool
	}{
		// Test Data Array:
		{
			name: "Test 1",
			rs:   ds,
			args: args{
				ip: "1.2.3.4",
			},
			want: []metal.Machine{
				testdata.M1,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &v1.FindMachinesRequest{
				NetworkIPs: []string{tt.args.ip},
			}
			got, err := tt.rs.FindMachines(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RethinkStore.FindMachines() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RethinkStore.FindMachines() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRethinkStore_SearchMachine5(t *testing.T) {

	// Mock the DB:
	ds, mock := InitMockDB()
	testdata.InitMockDBData(mock)

	mock.On(r.DB("mockdb").Table("machine").Filter(func(m r.Term) r.Term {
		return m.Field("allocation").Field("networks").Map(func(nw r.Term) r.Term {
			return nw.Field("prefixes")
		}).Contains(r.Expr("1.1.1.1/32"))
	})).Return([]metal.Machine{
		testdata.M1,
	}, nil)

	type args struct {
		prefix string
	}
	tests := []struct {
		name    string
		rs      *RethinkStore
		args    args
		want    []metal.Machine
		wantErr bool
	}{
		// Test Data Array:
		{
			name: "Test 1",
			rs:   ds,
			args: args{
				prefix: "1.1.1.1/32",
			},
			want: []metal.Machine{
				testdata.M1,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &v1.FindMachinesRequest{
				NetworkPrefixes: []string{tt.args.prefix},
			}
			got, err := tt.rs.FindMachines(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RethinkStore.FindMachines() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RethinkStore.FindMachines() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRethinkStore_SearchMachine6(t *testing.T) {

	// Mock the DB:
	ds, mock := InitMockDB()
	testdata.InitMockDBData(mock)

	mock.On(r.DB("mockdb").Table("machine").Filter(func(m r.Term) r.Term {
		return m.Field("hardware").Field("network_interfaces").Map(func(nic r.Term) r.Term {
			return nic.Field("neighbors").Map(func(neigh r.Term) r.Term {
				return neigh.Field("macAddress")
			})
		}).Contains(r.Expr("21:11:11:11:11:11"))
	})).Return([]metal.Machine{
		testdata.M1,
	}, nil)

	type args struct {
		mac string
	}
	tests := []struct {
		name    string
		rs      *RethinkStore
		args    args
		want    []metal.Machine
		wantErr bool
	}{
		// Test Data Array:
		{
			name: "Test 1",
			rs:   ds,
			args: args{
				mac: "21:11:11:11:11:11",
			},
			want: []metal.Machine{
				testdata.M1,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &v1.FindMachinesRequest{
				NicsNeighborMacAddresses: []string{tt.args.mac},
			}
			got, err := tt.rs.FindMachines(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RethinkStore.FindMachines() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RethinkStore.FindMachines() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRethinkStore_ListMachines(t *testing.T) {

	// Mock the DB:
	ds, mock := InitMockDB()
	testdata.InitMockDBData(mock)

	tests := []struct {
		name    string
		rs      *RethinkStore
		want    []metal.Machine
		wantErr bool
	}{
		// Test Data Array
		{
			name:    "Test 1",
			rs:      ds,
			want:    testdata.TestMachines,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.rs.ListMachines()
			if (err != nil) != tt.wantErr {
				t.Errorf("RethinkStore.ListMachines() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RethinkStore.ListMachines() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRethinkStore_CreateMachine(t *testing.T) {

	// mock the DBs
	ds, mock := InitMockDB()
	testdata.InitMockDBData(mock)

	type args struct {
		d *metal.Machine
	}
	tests := []struct {
		name    string
		rs      *RethinkStore
		args    args
		wantErr bool
	}{
		// Test Data Array:
		{
			name: "Test 1",
			rs:   ds,
			args: args{
				&testdata.M4,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.rs.CreateMachine(tt.args.d); (err != nil) != tt.wantErr {
				t.Errorf("RethinkStore.CreateMachine() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRethinkStore_DeleteMachine(t *testing.T) {

	// Mock the DB:
	ds, mock := InitMockDB()
	testdata.InitMockDBData(mock)

	tests := []struct {
		name    string
		rs      *RethinkStore
		machine *metal.Machine
		wantErr bool
	}{
		// Test Data Array:
		{
			name:    "Test 1",
			rs:      ds,
			machine: &testdata.M1,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rs.DeleteMachine(tt.machine)
			if (err != nil) != tt.wantErr {
				t.Errorf("RethinkStore.DeleteMachine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestRethinkStore_UpdateMachine(t *testing.T) {

	// Mock the DB:
	ds, mock := InitMockDB()
	testdata.InitMockDBData(mock)

	type args struct {
		oldD *metal.Machine
		newD *metal.Machine
	}
	tests := []struct {
		name    string
		rs      *RethinkStore
		args    args
		wantErr bool
	}{
		// Test Data Array:
		{
			name: "Test 1",
			rs:   ds,
			args: args{
				oldD: &testdata.M1,
				newD: &testdata.M2,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.rs.UpdateMachine(tt.args.oldD, tt.args.newD); (err != nil) != tt.wantErr {
				t.Errorf("RethinkStore.UpdateMachine() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRethinkStore_InsertWaitingMachine(t *testing.T) {

	// Mock the DB:
	ds, mock := InitMockDB()
	testdata.InitMockDBData(mock)

	tests := []struct {
		name    string
		rs      *RethinkStore
		machine *metal.Machine
		wantErr bool
	}{
		// Test Data Array:
		{
			name:    "Test 1",
			rs:      ds,
			machine: &testdata.M1,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.rs.InsertWaitingMachine(tt.machine); (err != nil) != tt.wantErr {
				t.Errorf("RethinkStore.InsertWaitingMachine() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRethinkStore_RemoveWaitingMachine(t *testing.T) {

	// Mock the DB:
	ds, mock := InitMockDB()
	testdata.InitMockDBData(mock)

	tests := []struct {
		name    string
		rs      *RethinkStore
		machine *metal.Machine
		wantErr bool
	}{
		// Test Data Array:
		{
			name:    "Test 1",
			rs:      ds,
			machine: &testdata.M1,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.rs.RemoveWaitingMachine(tt.machine); (err != nil) != tt.wantErr {
				t.Errorf("RethinkStore.RemoveWaitingMachine() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TODO: Add tests for UpdateWaitingMachine, WaitForMachineAllocation, FindAvailableMachine
