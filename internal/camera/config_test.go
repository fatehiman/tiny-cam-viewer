package camera

import "testing"

func TestExpand(t *testing.T) {
	cases := []struct{ in, normal, full string }{
		{"rtsp://h/a=1&stream={normal:1}{full:2}.sdp?x", "rtsp://h/a=1&stream=1.sdp?x", "rtsp://h/a=1&stream=2.sdp?x"},
		{"rtsp://h/ch={normal:1}&s={normal:1}{full:2}", "rtsp://h/ch=1&s=1", "rtsp://h/ch=&s=2"},
		{"rtsp://h/c={full:101}{normal:102}", "rtsp://h/c=102", "rtsp://h/c=101"},
		{"rtsp://h/plain.sdp", "rtsp://h/plain.sdp", ""},
		{"rtsp://h/s={normal:1}", "rtsp://h/s=1", ""},
		{"rtsp://h/s={NORMAL:1}{Full:2}", "rtsp://h/s=1", "rtsp://h/s=2"},
		{"rtsp://h/s={normal:1}{full:1}", "rtsp://h/s=1", ""},
	}
	for _, c := range cases {
		n, f := Expand(c.in)
		if n != c.normal || f != c.full {
			t.Errorf("Expand(%q) = %q, %q; want %q, %q", c.in, n, f, c.normal, c.full)
		}
	}
}

func TestParseAspectRatio(t *testing.T) {
	cases := []struct {
		in         string
		normalAR   string
		fullAR     string
	}{
		{"", "", ""},
		{"16:9", "16:9", "16:9"},
		{"4:3", "4:3", "4:3"},
		{"640x480", "640x480", "640x480"},
		{"{normal:4:3}{full:16:9}", "4:3", "16:9"},
		{"{normal:640x480}{full:1920x1080}", "640x480", "1920x1080"},
		{"{NORMAL:4:3}{FULL:16:9}", "4:3", "16:9"},
		{"{normal:4:3}", "4:3", "4:3"},
		{"{full:16:9}", "", "16:9"},
	}
	for _, c := range cases {
		n, f := parseAspectRatio(c.in)
		if n != c.normalAR || f != c.fullAR {
			t.Errorf("parseAspectRatio(%q) = %q, %q; want %q, %q", c.in, n, f, c.normalAR, c.fullAR)
		}
	}
}

func TestNormalizeAR(t *testing.T) {
	cases := []struct {
		in     string
		expect string
	}{
		{"", ""},
		{"16:9", "1.777778"},
		{"4:3", "1.333333"},
		{"16x9", "1.777778"},
		{"4x3", "1.333333"},
		{"640x480", "1.333333"},
		{"1920x1080", "1.777778"},
		{"1.333333", "1.333333"},
		{"1.777778", "1.777778"},
		{"invalid", ""},
		{"16:", ""},
		{":9", ""},
	}
	for _, c := range cases {
		result := normalizeAR(c.in)
		if result != c.expect {
			t.Errorf("normalizeAR(%q) = %q; want %q", c.in, result, c.expect)
		}
	}
}
