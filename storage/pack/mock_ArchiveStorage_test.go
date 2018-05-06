// Code generated by mockery v1.0.0. DO NOT EDIT.
package pack

import mock "github.com/stretchr/testify/mock"

// MockArchiveStorage is an autogenerated mock type for the ArchiveStorage type
type MockArchiveStorage struct {
	mock.Mock
}

// Create provides a mock function with given fields: name
func (_m *MockArchiveStorage) Create(name string) (File, error) {
	ret := _m.Called(name)

	var r0 File
	if rf, ok := ret.Get(0).(func(string) File); ok {
		r0 = rf(name)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(File)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(name)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// Delete provides a mock function with given fields: name
func (_m *MockArchiveStorage) Delete(name string) error {
	ret := _m.Called(name)

	var r0 error
	if rf, ok := ret.Get(0).(func(string) error); ok {
		r0 = rf(name)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// List provides a mock function with given fields:
func (_m *MockArchiveStorage) List() ([]string, error) {
	ret := _m.Called()

	var r0 []string
	if rf, ok := ret.Get(0).(func() []string); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]string)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func() error); ok {
		r1 = rf()
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// MaxParallel provides a mock function with given fields:
func (_m *MockArchiveStorage) MaxParallel() uint64 {
	ret := _m.Called()

	var r0 uint64
	if rf, ok := ret.Get(0).(func() uint64); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(uint64)
	}

	return r0
}

// MaxSize provides a mock function with given fields:
func (_m *MockArchiveStorage) MaxSize() uint64 {
	ret := _m.Called()

	var r0 uint64
	if rf, ok := ret.Get(0).(func() uint64); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(uint64)
	}

	return r0
}

// Open provides a mock function with given fields: name
func (_m *MockArchiveStorage) Open(name string) (File, error) {
	ret := _m.Called(name)

	var r0 File
	if rf, ok := ret.Get(0).(func(string) File); ok {
		r0 = rf(name)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(File)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(string) error); ok {
		r1 = rf(name)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}