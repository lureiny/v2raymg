package http

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	gomonkey "github.com/agiledragon/gomonkey/v2"
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/cluster"

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
						convey.So(expireTime, convey.ShouldEqual, 123)
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
						convey.So(expireTime, convey.ShouldEqual, 123+12)
					})
				},
				mockFunc: func() func() {
					patch1 := gomonkey.ApplyFunc(time.Now, func() time.Time {
						return time.Unix(12, 0)
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
					convey.Convey("get expire time by ttl with expire", func() {
						expireTime, err := getExpireTime(p)
						convey.So(err, convey.ShouldBeNil)
						convey.So(expireTime, convey.ShouldEqual, 123+12)
					})
				},
				mockFunc: func() func() {
					patch1 := gomonkey.ApplyFunc(time.Now, func() time.Time {
						return time.Unix(12, 0)
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
						convey.So(expireTime, convey.ShouldEqual, 0)
					})
				},
			},
			{
				p: map[string]string{},
				f: func(p map[string]string) {
					convey.Convey("get expire time with out expire and ttl", func() {
						expireTime, err := getExpireTime(p)
						convey.So(err, convey.ShouldBeNil)
						convey.So(expireTime, convey.ShouldEqual, 0)
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

func getResponse(resp *http.Response) []byte {
	d, _ := ioutil.ReadAll(resp.Body)
	return d
}

func setGlobalServer() {
	GlobalHttpServer.Host = "127.0.0.1"
	GlobalHttpServer.Port = 10000
}

func startGlobalHttpServer() {
	setGlobalServer()
	go GlobalHttpServer.Start()
	time.Sleep(100 * time.Microsecond)
}

func TestUserHandleFunc(t *testing.T) {
	type TestCase struct {
		f        func()
		mockFunc func() func()
	}
	startGlobalHttpServer()

	convey.Convey("test user handler func", t, func() {
		testCases := []TestCase{
			{
				f: func() {
					convey.Convey("get expire time fail", func() {
						resp, _ := http.Get("http://127.0.0.1:10000/user")
						convey.So(string(getResponse(resp)), convey.ShouldEqual, "illegal expire time > invalid expire time")
					})
				},
				mockFunc: func() func() {
					patch1 := gomonkey.ApplyPrivateMethod(&UserHandler{}, "parseParam", func(_ *gin.Context) map[string]string {
						return map[string]string{}
					})
					patch2 := gomonkey.ApplyFunc(getExpireTime, func(map[string]string) (int64, error) {
						return 0, fmt.Errorf("invalid expire time")
					})
					return func() {
						patch1.Reset()
						patch2.Reset()
					}
				},
			},
			{
				f: func() {
					convey.Convey("get node fail", func() {
						resp, _ := http.Get("http://127.0.0.1:10000/user")
						convey.So(string(getResponse(resp)), convey.ShouldEqual, "no avaliable node")
					})
				},
				mockFunc: func() func() {
					patch1 := gomonkey.ApplyPrivateMethod(&UserHandler{}, "parseParam", func(_ *gin.Context) map[string]string {
						return map[string]string{
							"target": "",
						}
					})
					patch2 := gomonkey.ApplyFunc(getExpireTime, func(map[string]string) (int64, error) {
						return 0, nil
					})
					patch3 := gomonkey.ApplyPrivateMethod(&HttpServer{}, "getTargetNodes", func(target string) *[]*cluster.Node {
						return &[]*cluster.Node{}
					})
					return func() {
						patch1.Reset()
						patch2.Reset()
						patch3.Reset()
					}
				},
			},
		}

		for _, t := range testCases {
			var cb func() = nil
			if t.mockFunc != nil {
				cb = t.mockFunc()
			}
			t.f()
			if cb != nil {
				cb()
			}
		}
	})
}
