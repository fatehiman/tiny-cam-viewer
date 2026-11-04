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
