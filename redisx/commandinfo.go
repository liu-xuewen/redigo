// Copyright 2014 Gary Burd
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.
/*
除非适用法律要求或书面同意，否则根据本许可证分发的软件是按“原样”分发的，没有任何明示或暗示的任何类型的担保或条件。
请参阅许可证下管理权限和限制的特定语言的许可证。
*/

package redisx

import (
	"strings"
)

const (
	connectionWatchState = 1 << iota
	connectionMultiState
	connectionSubscribeState
	connectionMonitorState
)

type commandInfo struct {
	notMuxable bool
}

var commandInfos = map[string]commandInfo{
	"WATCH":      {notMuxable: true},
	"UNWATCH":    {notMuxable: true},
	"MULTI":      {notMuxable: true},
	"EXEC":       {notMuxable: true},
	"DISCARD":    {notMuxable: true},
	"PSUBSCRIBE": {notMuxable: true},
	"SUBSCRIBE":  {notMuxable: true},
	"MONITOR":    {notMuxable: true},
}

func init() {
	for n, ci := range commandInfos {
		commandInfos[strings.ToLower(n)] = ci
	}
}

func lookupCommandInfo(commandName string) commandInfo {
	if ci, ok := commandInfos[commandName]; ok {
		return ci
	}
	return commandInfos[strings.ToUpper(commandName)]
}
