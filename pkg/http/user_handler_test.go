package http

import (
	"testing"
	"time"

	gomonkey "github.com/agiledragon/gomonkey/v2"
	"github.com/smartystreets/goconvey/convey"
)

func TestGetExpireTime(t *testing.T) {
	type TestCase struct {
		f        func(map[string]string)
		p        map[string]string
		mockFunc func() func()
	}
	convey.Convey("get expire time", t, func() {
		testCases := []TestCase{
			{
				p: map[string]string{
					"expire": "123",
				},
				f: func(p map[string]string) {
					convey.Convey("get expire time by expire without ttl", func() {
						expireTime, err := getExpireTime(p)
						convey.So(err, convey.ShouldBeNil)
						convey.So(expireTime, convey.ShouldEqual, 123)
					})
				},
			},
			{
				p: map[string]string{
					"ttl": "123",
				},
				f: func(p map[string]string) {
					convey.Convey("get expire time by ttl without expire", func() {
						expireTime, err := getExpireTime(p)
						convey.So(err, convey.ShouldBeNil)
						convey.So(expireTime, convey.ShouldEqual, 123+12)
					})
				},
				mockFunc: func() func() {
					patch := gomonkey.ApplyFunc(time.Now, func() time.Time {
						return time.Unix(12, 0)
					})
					return patch.Reset
				},
			},
			{
				p: map[string]string{
					"ttl":    "0",
					"expire": "1234",
				},
				f: func(p map[string]string) {
					convey.Convey("prefer expire when ttl is zero", func() {
						expireTime, err := getExpireTime(p)
						convey.So(err, convey.ShouldBeNil)
						convey.So(expireTime, convey.ShouldEqual, 1234)
					})
				},
			},
			{
				p: map[string]string{
					"ttl":    "123",
					"expire": "1234",
				},
				f: func(p map[string]string) {
					convey.Convey("prefer ttl when both provided", func() {
						expireTime, err := getExpireTime(p)
						convey.So(err, convey.ShouldBeNil)
						convey.So(expireTime, convey.ShouldEqual, 123+12)
					})
				},
				mockFunc: func() func() {
					patch := gomonkey.ApplyFunc(time.Now, func() time.Time {
						return time.Unix(12, 0)
					})
					return patch.Reset
				},
			},
			{
				p: map[string]string{
					"ttl":    "0",
					"expire": "0",
				},
				f: func(p map[string]string) {
					convey.Convey("both zero returns zero", func() {
						expireTime, err := getExpireTime(p)
						convey.So(err, convey.ShouldBeNil)
						convey.So(expireTime, convey.ShouldEqual, 0)
					})
				},
			},
			{
				p: map[string]string{},
				f: func(p map[string]string) {
					convey.Convey("empty params returns zero", func() {
						expireTime, err := getExpireTime(p)
						convey.So(err, convey.ShouldBeNil)
						convey.So(expireTime, convey.ShouldEqual, 0)
					})
				},
			},
			{
				p: map[string]string{
					"expire": "notanumber",
				},
				f: func(p map[string]string) {
					convey.Convey("invalid expire returns error", func() {
						_, err := getExpireTime(p)
						convey.So(err, convey.ShouldNotBeNil)
					})
				},
			},
		}

		for _, tc := range testCases {
			var reset func()
			if tc.mockFunc != nil {
				reset = tc.mockFunc()
			}
			tc.f(tc.p)
			if reset != nil {
				reset()
			}
		}
	})
}
