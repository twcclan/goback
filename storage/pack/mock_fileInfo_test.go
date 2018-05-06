// Code generated by mockery v1.0.0. DO NOT EDIT.
package pack

import mock "github.com/stretchr/testify/mock"
import os "os"
import time "time"

// mockFileInfo is an autogenerated mock type for the fileInfo type
type mockFileInfo struct {
	mock.Mock
}

// IsDir provides a mock function with given fields:
func (_m *mockFileInfo) IsDir() bool {
	ret := _m.Called()

	var r0 bool
	if rf, ok := ret.Get(0).(func() bool); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(bool)
	}

	return r0
}

// ModTime provides a mock function with given fields:
func (_m *mockFileInfo) ModTime() time.Time {
	ret := _m.Called()

	var r0 time.Time
	if rf, ok := ret.Get(0).(func() time.Time); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(time.Time)
	}

	return r0
}

// Mode provides a mock function with given fields:
func (_m *mockFileInfo) Mode() os.FileMode {
	ret := _m.Called()

	var r0 os.FileMode
	if rf, ok := ret.Get(0).(func() os.FileMode); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(os.FileMode)
	}

	return r0
}

// Name provides a mock function with given fields:
func (_m *mockFileInfo) Name() string {
	ret := _m.Called()

	var r0 string
	if rf, ok := ret.Get(0).(func() string); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(string)
	}

	return r0
}

// Size provides a mock function with given fields:
func (_m *mockFileInfo) Size() int64 {
	ret := _m.Called()

	var r0 int64
	if rf, ok := ret.Get(0).(func() int64); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(int64)
	}

	return r0
}

// Sys provides a mock function with given fields:
func (_m *mockFileInfo) Sys() interface{} {
	ret := _m.Called()

	var r0 interface{}
	if rf, ok := ret.Get(0).(func() interface{}); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(interface{})
		}
	}

	return r0
}
