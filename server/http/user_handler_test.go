package http

import (
	"testing"
	"time"

	"github.com/agiledragon/gomonkey"
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
					convey.Convey("get expire time by expire with out ttl", func() {
						expireTime, err := getExpireTime(p)
						convey.So(err, convey.ShouldBeNil)
						convey.ShouldEqual(expireTime, 123)
					})
				},
			},
			{
				p: map[string]string{
					"ttl": "123",
				},
				f: func(p map[string]string) {
					convey.Convey("get expire time by ttl with out expire", func() {
						expireTime, err := getExpireTime(p)
						convey.So(err, convey.ShouldBeNil)
						convey.ShouldEqual(expireTime, 123+12)
					})
				},
				mockFunc: func() func() {
					patch1 := gomonkey.ApplyFunc(time.Time.Unix, func(time.Time) int64 {
						return 12
					})
					return func() {
						patch1.Reset()
					}
				},
			},
			{
				p: map[string]string{
					"ttl":    "0",
					"expire": "1234",
				},
				f: func(p map[string]string) {
					convey.Convey("get expire time by expire with ttl", func() {
						expireTime, err := getExpireTime(p)
						convey.So(err, convey.ShouldBeNil)
						convey.ShouldEqual(expireTime, 1234)
					})
				},
			},
			{
				p: map[string]string{
					"ttl":    "123",
					"expire": "1234",
				},
				f: func(p map[string]string) {
					convey.Convey("get expire time by ttl with expire", func() {
						expireTime, err := getExpireTime(p)
						convey.So(err, convey.ShouldBeNil)
						convey.ShouldEqual(expireTime, 123+12)
					})
				},
				mockFunc: func() func() {
					patch1 := gomonkey.ApplyFunc(time.Time.Unix, func(time.Time) int64 {
						return 12
					})
					return func() {
						patch1.Reset()
					}
				},
			},
			{
				p: map[string]string{
					"ttl":    "0",
					"expire": "0",
				},
				f: func(p map[string]string) {
					convey.Convey("get expire time by default expire and ttl", func() {
						expireTime, err := getExpireTime(p)
						convey.So(err, convey.ShouldBeNil)
						convey.ShouldEqual(expireTime, 0)
					})
				},
			},
			{
				p: map[string]string{},
				f: func(p map[string]string) {
					convey.Convey("get expire time with out expire and ttl", func() {
						expireTime, err := getExpireTime(p)
						convey.So(err, convey.ShouldBeNil)
						convey.ShouldEqual(expireTime, 0)
					})
				},
			},
		}

		for _, t := range testCases {
			var cb func() = nil
			if t.mockFunc != nil {
				cb = t.mockFunc()
			}
			t.f(t.p)
			if cb != nil {
				cb()
			}
		}
	})

}
