// Code generated by mockery v1.0.0. DO NOT EDIT.
package backup

import mock "github.com/stretchr/testify/mock"
import proto "github.com/twcclan/goback/proto"

// MockObjectStore is an autogenerated mock type for the ObjectStore type
type MockObjectStore struct {
	mock.Mock
}

// Delete provides a mock function with given fields: _a0
func (_m *MockObjectStore) Delete(_a0 *proto.Ref) error {
	ret := _m.Called(_a0)

	var r0 error
	if rf, ok := ret.Get(0).(func(*proto.Ref) error); ok {
		r0 = rf(_a0)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// Get provides a mock function with given fields: _a0
func (_m *MockObjectStore) Get(_a0 *proto.Ref) (*proto.Object, error) {
	ret := _m.Called(_a0)

	var r0 *proto.Object
	if rf, ok := ret.Get(0).(func(*proto.Ref) *proto.Object); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*proto.Object)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(*proto.Ref) error); ok {
		r1 = rf(_a0)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// Put provides a mock function with given fields: _a0
func (_m *MockObjectStore) Put(_a0 *proto.Object) error {
	ret := _m.Called(_a0)

	var r0 error
	if rf, ok := ret.Get(0).(func(*proto.Object) error); ok {
		r0 = rf(_a0)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// Walk provides a mock function with given fields: _a0, _a1, _a2
func (_m *MockObjectStore) Walk(_a0 bool, _a1 proto.ObjectType, _a2 ObjectReceiver) error {
	ret := _m.Called(_a0, _a1, _a2)

	var r0 error
	if rf, ok := ret.Get(0).(func(bool, proto.ObjectType, ObjectReceiver) error); ok {
		r0 = rf(_a0, _a1, _a2)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}
