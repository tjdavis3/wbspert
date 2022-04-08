package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestSheet_GetParents(t *testing.T) {
	type fields struct {
		WBS      string
		Title    string
		Parents  string
		Duration float32
	}
	tests := []struct {
		name   string
		fields fields
		want   []string
	}{
		{
			name:   "Test with spaces",
			fields: fields{WBS: "1.1", Title: "Test", Parents: "2.1.1, 3.1.1", Duration: 4},
			want:   []string{"2.1.1", "3.1.1"},
		},
		{
			name:   "Test with extra spaces",
			fields: fields{WBS: "1.1", Title: "Test", Parents: "2.1.1,  3.1.1, 4.1.2", Duration: 4},
			want:   []string{"2.1.1", "3.1.1", "4.1.2"},
		},
		{
			name:   "Test with no spaces",
			fields: fields{WBS: "1.1", Title: "Test", Parents: "2.1.1,3.1.1", Duration: 4},
			want:   []string{"2.1.1", "3.1.1"},
		},
		{
			name:   "Test no parents",
			fields: fields{WBS: "1.1", Title: "Test", Duration: 4},
			want:   strings.Split("", ","),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Sheet{
				WBS:      tt.fields.WBS,
				Title:    tt.fields.Title,
				Parents:  tt.fields.Parents,
				Duration: tt.fields.Duration,
			}
			if got := s.GetParents(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Sheet.GetParents() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSheet_GetPertNode(t *testing.T) {
	type fields struct {
		WBS      string
		Title    string
		Parents  string
		Duration float32
	}
	field := fields{WBS: "1.1", Title: "Test", Parents: "2.1.1, 3.1.1", Duration: 4}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			name:   "Get a node",
			fields: field,
			want:   fmt.Sprintf(pertNode, field.WBS, field.Title, field.WBS, "", "", field.Duration),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Sheet{
				WBS:      tt.fields.WBS,
				Title:    tt.fields.Title,
				Parents:  tt.fields.Parents,
				Duration: tt.fields.Duration,
			}
			if got := s.GetPertNode(); got != tt.want {
				t.Errorf("Sheet.GetPertNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSheet_GetPertLevel(t *testing.T) {
	type fields struct {
		WBS      string
		Title    string
		Parents  string
		Duration float32
	}
	field := fields{WBS: "1.1", Title: "Test", Parents: "2.1.1, 3.1.1", Duration: 4}

	type args struct {
		lvl int
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   string
	}{
		{
			name:   "Test level 2",
			fields: field,
			args:   args{lvl: 2},
			want:   fmt.Sprintf(pertNode, field.WBS, field.Title, field.WBS, "", "", field.Duration),
		},
		{
			name:   "Test level 3",
			fields: field,
			args:   args{lvl: 3},
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Sheet{
				WBS:      tt.fields.WBS,
				Title:    tt.fields.Title,
				Parents:  tt.fields.Parents,
				Duration: tt.fields.Duration,
			}
			if got := s.GetPertLevel(tt.args.lvl); got != tt.want {
				t.Errorf("Sheet.GetPertLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_genMarkdownTableHeader(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: "Test Markdown Header",
			want: `| WBS | Task | Parents | Duration |
| --- | ---- | ------- | -------- |`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := genMarkdownTableHeader(); got != tt.want {
				t.Errorf("genMarkdownTableHeader() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSheet_GetWBS(t *testing.T) {
	type fields struct {
		WBS      string
		Title    string
		Parents  string
		Duration float32
		Status   string
	}
	l2field := fields{WBS: "1.1", Title: "Test", Parents: "2.1.1, 3.1.1", Duration: 4}
	l3field := fields{WBS: "1.1.2", Title: "Test2", Parents: "2.1.1, 3.1.1", Duration: 4}

	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			name:   "Test level 2",
			fields: l2field,
			want:   "** 1.1: Test",
		},
		{
			name:   "Test level 3",
			fields: l3field,
			want:   "*** 1.1.2: Test2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Sheet{
				WBS:      tt.fields.WBS,
				Title:    tt.fields.Title,
				Parents:  tt.fields.Parents,
				Duration: tt.fields.Duration,
			}
			if got := s.GetWBS(); got != tt.want {
				t.Errorf("Sheet.GetWBS() = %v, want %v", got, tt.want)
			}
		})
	}
}
