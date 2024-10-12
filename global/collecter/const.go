package collecter

import "github.com/lureiny/v2raymg/ping"

// from https://joys.name/2010/06/china-unicom-china-telecom-china-mobile-ip-address.html

var telecomPingNodes = []*ping.PingNodeInfo{
	// {
	// 	Host: "219.141.136.10",
	// 	Geo:  "China, 北京市(北京市)",
	// 	ISP:  "Telecom",
	// },
	{
		Host: "221.238.6.1",
		Geo:  "China, 天津市(天津市)",
		ISP:  "Telecom",
	},
	{
		Host: "124.238.251.165",
		Geo:  "China, 河北省(廊坊市)",
		ISP:  "Telecom",
	},
	// {
	// 	Host: "219.149.144.1",
	// 	Geo:  "China, 山西省(太原市)",
	// 	ISP:  "Telecom",
	// },
	// {
	// 	Host: "219.148.162.1",
	// 	Geo:  "China, 内蒙古自治区(呼和浩特市)",
	// 	ISP:  "Telecom",
	// },
	{
		Host: "219.148.204.66",
		Geo:  "China, 辽宁省(沈阳市)",
		ISP:  "Telecom",
	},
	{
		Host: "222.168.1.145",
		Geo:  "China, 吉林省(长春市)",
		ISP:  "Telecom",
	},
	{
		Host: "219.147.198.230",
		Geo:  "China, 黑龙江省(齐齐哈尔市)",
		ISP:  "Telecom",
	},
	{
		Host: "202.96.209.133",
		Geo:  "China, 上海市(上海市)",
		ISP:  "Telecom",
	},
	{
		Host: "218.2.2.2",
		Geo:  "China, 江苏省(南京市)",
		ISP:  "Telecom",
	},
	{
		Host: "122.224.205.1",
		Geo:  "China, 浙江省(杭州市)",
		ISP:  "Telecom",
	},
	{
		Host: "202.102.194.1",
		Geo:  "China, 安徽省(合肥市)",
		ISP:  "Telecom",
	},
	{
		Host: "218.85.157.99",
		Geo:  "China, 福建省(福州市)",
		ISP:  "Telecom",
	},
	// {
	// 	Host: "218.65.103.1",
	// 	Geo:  "China, 江西省(南昌市)",
	// 	ISP:  "Telecom",
	// },
	{
		Host: "222.173.95.53",
		Geo:  "China, 山东省(青岛市)",
		ISP:  "Telecom",
	},
	{
		Host: "222.85.126.1",
		Geo:  "China, 河南省(郑州市)",
		ISP:  "Telecom",
	},
	{
		Host: "61.183.1.1",
		Geo:  "China, 湖北省(武汉市)",
		ISP:  "Telecom",
	},
	// {
	// 	Host: "",
	// 	Geo:  "China, 湖南省(长沙市)",
	// 	ISP:  "Telecom",
	// },
	{
		Host: "202.96.128.86",
		Geo:  "China, 广东省(广州市)",
		ISP:  "Telecom",
	},
	// {
	// 	Host: "58.59.135.1",
	// 	Geo:  "China, 广西壮族自治区(南宁市)",
	// 	ISP:  "Telecom",
	// },
	{
		Host: "202.100.220.254",
		Geo:  "China, 海南省(海口市)",
		ISP:  "Telecom",
	},
	// {
	// 	Host: "218.70.65.254",
	// 	Geo:  "China, 重庆市(重庆市)",
	// 	ISP:  "Telecom",
	// },
	{
		Host: "61.139.2.69",
		Geo:  "China, 四川省(成都市)",
		ISP:  "Telecom",
	},
	{
		Host: "202.98.192.67",
		Geo:  "China, 贵州省(贵阳市)",
		ISP:  "Telecom",
	},
	{
		Host: "222.172.200.68",
		Geo:  "China, 云南省(昆明市)",
		ISP:  "Telecom",
	},
	{
		Host: "219.151.48.1",
		Geo:  "China, 西藏自治区(拉萨市)",
		ISP:  "Telecom",
	},
	{
		Host: "125.76.216.1",
		Geo:  "China, 陕西省(西安市)",
		ISP:  "Telecom",
	},
	{
		Host: "202.100.64.66",
		Geo:  "China, 甘肃省(兰州市)",
		ISP:  "Telecom",
	},
	{
		Host: "202.100.138.68",
		Geo:  "China, 青海省(西宁市)",
		ISP:  "Telecom",
	},
	{
		Host: "202.100.96.68",
		Geo:  "China, 宁夏回族自治区(银川市)",
		ISP:  "Telecom",
	},
	{
		Host: "61.128.97.1",
		Geo:  "China, 新疆维吾尔自治区(乌鲁木齐市)",
		ISP:  "Telecom",
	},
}

var unicomPingNodes = []*ping.PingNodeInfo{
	{
		Host: "202.96.18.1",
		Geo:  "China, 北京市(北京市)",
		ISP:  "China Unicom",
	},
	// {
	// 	Host: "218.69.33.1",
	// 	Geo:  "China, 天津市(天津市)",
	// 	ISP:  "China Unicom",
	// },
	{
		Host: "202.99.160.68",
		Geo:  "China, 河北省(石家庄市)",
		ISP:  "China Unicom",
	},
	{
		Host: "218.26.176.1",
		Geo:  "China, 山西省(太原市)",
		ISP:  "China Unicom",
	},
	{
		Host: "202.99.224.68",
		Geo:  "China, 内蒙古自治区(呼和浩特市)",
		ISP:  "China Unicom",
	},
	// {
	// 	Host: "202.96.75.1",
	// 	Geo:  "China, 辽宁省(沈阳市)",
	// 	ISP:  "China Unicom",
	// },
	{
		Host: "202.98.0.68",
		Geo:  "China, 吉林省(长春市)",
		ISP:  "China Unicom",
	},
	{
		Host: "211.93.24.129",
		Geo:  "China, 黑龙江省(哈尔滨市)",
		ISP:  "China Unicom",
	},
	{
		Host: "211.95.72.1",
		Geo:  "China, 上海市(上海市)",
		ISP:  "China Unicom",
	},
	{
		Host: "58.240.40.1",
		Geo:  "China, 江苏省(南京市)",
		ISP:  "China Unicom",
	},
	{
		Host: "221.12.1.227",
		Geo:  "China, 浙江省(杭州市)",
		ISP:  "China Unicom",
	},
	{
		Host: "58.242.2.2",
		Geo:  "China, 安徽省(合肥市)",
		ISP:  "China Unicom",
	},
	{
		Host: "58.22.96.1",
		Geo:  "China, 福建省(福州市)",
		ISP:  "China Unicom",
	},
	{
		Host: "220.248.192.13",
		Geo:  "China, 江西省(南昌市)",
		ISP:  "China Unicom",
	},
	{
		Host: "202.102.134.68",
		Geo:  "China, 山东省(青岛市)",
		ISP:  "China Unicom",
	},
	// {
	// 	Host: "125.46.62.1",
	// 	Geo:  "China, 河南省(郑州市)",
	// 	ISP:  "China Unicom",
	// },
	// {
	// 	Host: "218.104.111.254",
	// 	Geo:  "China, 湖北省(武汉市)",
	// 	ISP:  "China Unicom",
	// },
	{
		Host: "58.20.127.238",
		Geo:  "China, 湖南省(长沙市)",
		ISP:  "China Unicom",
	},
	{
		Host: "210.21.11.1",
		Geo:  "China, 广东省(广州市)",
		ISP:  "China Unicom",
	},
	{
		Host: "221.7.128.68",
		Geo:  "China, 广西壮族自治区(南宁市)",
		ISP:  "China Unicom",
	},
	{
		Host: "221.11.132.2",
		Geo:  "China, 海南省(海口市)",
		ISP:  "China Unicom",
	},
	{
		Host: "221.5.203.98",
		Geo:  "China, 重庆市(重庆市)",
		ISP:  "China Unicom",
	},
	{
		Host: "119.6.6.6",
		Geo:  "China, 四川省(成都市)",
		ISP:  "China Unicom",
	},
	// {
	// 	Host: "221.13.30.242",
	// 	Geo:  "China, 贵州省(贵阳市)",
	// 	ISP:  "China Unicom",
	// },
	// {
	// 	Host: "221.3.131.1",
	// 	Geo:  "China, 云南省(昆明市)",
	// 	ISP:  "China Unicom",
	// },
	{
		Host: "221.13.65.35",
		Geo:  "China, 西藏自治区(拉萨市)",
		ISP:  "China Unicom",
	},
	// {
	// 	Host: "221.11.1.1",
	// 	Geo:  "China, 陕西省(西安市)",
	// 	ISP:  "China Unicom",
	// },
	// {
	// 	Host: "221.7.34.1",
	// 	Geo:  "China, 甘肃省(兰州市)",
	// 	ISP:  "China Unicom",
	// },
	{
		Host: "221.207.55.1",
		Geo:  "China, 青海省(西宁市)",
		ISP:  "China Unicom",
	},
	{
		Host: "211.93.0.81",
		Geo:  "China, 宁夏回族自治区(银川市)",
		ISP:  "China Unicom",
	},
	{
		Host: "221.7.1.21",
		Geo:  "China, 新疆维吾尔自治区(乌鲁木齐市)",
		ISP:  "China Unicom",
	},
}
var mobilePingNodes = []*ping.PingNodeInfo{
	{
		Host: "221.130.33.60",
		Geo:  "China, 北京市(北京市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.137.160.1",
		Geo:  "China, 天津市(天津市)",
		ISP:  "Mobile",
	},
	{
		Host: "218.207.64.1",
		Geo:  "China, 河北省(衡水市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.142.29.1",
		Geo:  "China, 山西省(太原市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.138.91.1",
		Geo:  "China, 内蒙古自治区(呼和浩特市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.137.32.178",
		Geo:  "China, 辽宁省(沈阳市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.141.0.165",
		Geo:  "China, 吉林省(吉林市)",
		ISP:  "Mobile",
	},
	{
		Host: "218.203.59.116",
		Geo:  "China, 黑龙江省(哈尔滨市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.136.112.50",
		Geo:  "China, 上海市(上海市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.138.200.69",
		Geo:  "China, 江苏省(南京市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.140.19.1",
		Geo:  "China, 浙江省(杭州市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.138.180.3",
		Geo:  "China, 安徽省(合肥市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.138.151.161",
		Geo:  "China, 福建省(福州市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.141.85.68",
		Geo:  "China, 江西省(南昌市)",
		ISP:  "Mobile",
	},
	{
		Host: "218.201.96.130",
		Geo:  "China, 山东省(青岛市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.138.30.66",
		Geo:  "China, 河南省(郑州市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.137.58.20",
		Geo:  "China, 湖北省(武汉市)",
		ISP:  "Mobile",
	},
	// {
	// 	Host: "211.138.224.1",
	// 	Geo:  "China, 湖南省(长沙市)",
	// 	ISP:  "Mobile",
	// },
	{
		Host: "211.136.192.6",
		Geo:  "China, 广东省(广州市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.138.240.100",
		Geo:  "China, 广西壮族自治区(南宁市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.138.164.6",
		Geo:  "China, 海南省(海口市)",
		ISP:  "Mobile",
	},
	{
		Host: "218.201.17.2",
		Geo:  "China, 重庆市(重庆市)",
		ISP:  "Mobile",
	},
	{
		Host: "218.205.231.1",
		Geo:  "China, 四川省(成都市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.139.5.29",
		Geo:  "China, 贵州省(贵阳市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.139.29.150",
		Geo:  "China, 云南省(昆明市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.139.73.50",
		Geo:  "China, 西藏自治区(拉萨市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.137.130.2",
		Geo:  "China, 陕西省(西安市)",
		ISP:  "Mobile",
	},
	{
		Host: "218.203.160.194",
		Geo:  "China, 甘肃省(兰州市)",
		ISP:  "Mobile",
	},
	{
		Host: "211.138.75.123",
		Geo:  "China, 青海省(西宁市)",
		ISP:  "Mobile",
	},
	{
		Host: "218.203.123.116",
		Geo:  "China, 宁夏回族自治区(银川市)",
		ISP:  "Mobile",
	},
	{
		Host: "218.202.152.130",
		Geo:  "China, 新疆维吾尔自治区(乌鲁木齐市)",
		ISP:  "Mobile",
	},
}
