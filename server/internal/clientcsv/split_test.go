package clientcsv

import (
	"bufio"
	"reflect"
	"strings"
	"testing"
)

func TestLocalizedQuotesCannotConsumeFollowingCard(t *testing.T) {
	input := "1,\"台词,原样保留\",3\r\n2,\"\"台词,引号未配对\"\n3,下一张卡,4\n"
	expected := [][]string{{"1", `"台词,原样保留"`, "3"}, {"2", `""台词`, `引号未配对"`}, {"3", "下一张卡", "4"}}
	var actual [][]string
	lines := bufio.NewScanner(strings.NewReader(input))
	for lines.Scan() {
		actual = append(actual, SplitLine(lines.Text()))
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("following card lost or literal quotes changed: %#v", actual)
	}
}
