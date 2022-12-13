// Code generated by mockery v2.15.0. DO NOT EDIT.

package mocks

import (
	event "github.com/DataDog/datadog-agent/pkg/compliance/event"
	mock "github.com/stretchr/testify/mock"
)

// Reporter is an autogenerated mock type for the Reporter type
type Reporter struct {
	mock.Mock
}

// Report provides a mock function with given fields: _a0
func (_m *Reporter) Report(_a0 *event.Event) {
	_m.Called(_a0)
}

// ReportRaw provides a mock function with given fields: content, service, tags
func (_m *Reporter) ReportRaw(content []byte, service string, tags ...string) {
	_va := make([]interface{}, len(tags))
	for _i := range tags {
		_va[_i] = tags[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, content, service)
	_ca = append(_ca, _va...)
	_m.Called(_ca...)
}

type mockConstructorTestingTNewReporter interface {
	mock.TestingT
	Cleanup(func())
}

// NewReporter creates a new instance of Reporter. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewReporter(t mockConstructorTestingTNewReporter) *Reporter {
	mock := &Reporter{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
