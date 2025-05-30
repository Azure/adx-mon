// Copyright 2020, OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.6
// 	protoc        (unknown)
// source: opentelemetry/proto/collector/logs/v1/logs_service.proto

//go:build protoopaque

package logsv1

import (
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	unsafe "unsafe"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type ExportLogsServiceRequest struct {
	state                   protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_ResourceLogs *[]*v1.ResourceLogs    `protobuf:"bytes,1,rep,name=resource_logs,json=resourceLogs,proto3"`
	unknownFields           protoimpl.UnknownFields
	sizeCache               protoimpl.SizeCache
}

func (x *ExportLogsServiceRequest) Reset() {
	*x = ExportLogsServiceRequest{}
	mi := &file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ExportLogsServiceRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ExportLogsServiceRequest) ProtoMessage() {}

func (x *ExportLogsServiceRequest) ProtoReflect() protoreflect.Message {
	mi := &file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *ExportLogsServiceRequest) GetResourceLogs() []*v1.ResourceLogs {
	if x != nil {
		if x.xxx_hidden_ResourceLogs != nil {
			return *x.xxx_hidden_ResourceLogs
		}
	}
	return nil
}

func (x *ExportLogsServiceRequest) SetResourceLogs(v []*v1.ResourceLogs) {
	x.xxx_hidden_ResourceLogs = &v
}

type ExportLogsServiceRequest_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	// An array of ResourceLogs.
	// For data coming from a single resource this array will typically contain one
	// element. Intermediary nodes (such as OpenTelemetry Collector) that receive
	// data from multiple origins typically batch the data before forwarding further and
	// in that case this array will contain multiple elements.
	ResourceLogs []*v1.ResourceLogs
}

func (b0 ExportLogsServiceRequest_builder) Build() *ExportLogsServiceRequest {
	m0 := &ExportLogsServiceRequest{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_ResourceLogs = &b.ResourceLogs
	return m0
}

type ExportLogsServiceResponse struct {
	state                     protoimpl.MessageState    `protogen:"opaque.v1"`
	xxx_hidden_PartialSuccess *ExportLogsPartialSuccess `protobuf:"bytes,1,opt,name=partial_success,json=partialSuccess,proto3"`
	unknownFields             protoimpl.UnknownFields
	sizeCache                 protoimpl.SizeCache
}

func (x *ExportLogsServiceResponse) Reset() {
	*x = ExportLogsServiceResponse{}
	mi := &file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ExportLogsServiceResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ExportLogsServiceResponse) ProtoMessage() {}

func (x *ExportLogsServiceResponse) ProtoReflect() protoreflect.Message {
	mi := &file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *ExportLogsServiceResponse) GetPartialSuccess() *ExportLogsPartialSuccess {
	if x != nil {
		return x.xxx_hidden_PartialSuccess
	}
	return nil
}

func (x *ExportLogsServiceResponse) SetPartialSuccess(v *ExportLogsPartialSuccess) {
	x.xxx_hidden_PartialSuccess = v
}

func (x *ExportLogsServiceResponse) HasPartialSuccess() bool {
	if x == nil {
		return false
	}
	return x.xxx_hidden_PartialSuccess != nil
}

func (x *ExportLogsServiceResponse) ClearPartialSuccess() {
	x.xxx_hidden_PartialSuccess = nil
}

type ExportLogsServiceResponse_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	// The details of a partially successful export request.
	//
	// If the request is only partially accepted
	// (i.e. when the server accepts only parts of the data and rejects the rest)
	// the server MUST initialize the `partial_success` field and MUST
	// set the `rejected_<signal>` with the number of items it rejected.
	//
	// Servers MAY also make use of the `partial_success` field to convey
	// warnings/suggestions to senders even when the request was fully accepted.
	// In such cases, the `rejected_<signal>` MUST have a value of `0` and
	// the `error_message` MUST be non-empty.
	//
	// A `partial_success` message with an empty value (rejected_<signal> = 0 and
	// `error_message` = "") is equivalent to it not being set/present. Senders
	// SHOULD interpret it the same way as in the full success case.
	PartialSuccess *ExportLogsPartialSuccess
}

func (b0 ExportLogsServiceResponse_builder) Build() *ExportLogsServiceResponse {
	m0 := &ExportLogsServiceResponse{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_PartialSuccess = b.PartialSuccess
	return m0
}

type ExportLogsPartialSuccess struct {
	state                         protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_RejectedLogRecords int64                  `protobuf:"varint,1,opt,name=rejected_log_records,json=rejectedLogRecords,proto3"`
	xxx_hidden_ErrorMessage       string                 `protobuf:"bytes,2,opt,name=error_message,json=errorMessage,proto3"`
	unknownFields                 protoimpl.UnknownFields
	sizeCache                     protoimpl.SizeCache
}

func (x *ExportLogsPartialSuccess) Reset() {
	*x = ExportLogsPartialSuccess{}
	mi := &file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ExportLogsPartialSuccess) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ExportLogsPartialSuccess) ProtoMessage() {}

func (x *ExportLogsPartialSuccess) ProtoReflect() protoreflect.Message {
	mi := &file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *ExportLogsPartialSuccess) GetRejectedLogRecords() int64 {
	if x != nil {
		return x.xxx_hidden_RejectedLogRecords
	}
	return 0
}

func (x *ExportLogsPartialSuccess) GetErrorMessage() string {
	if x != nil {
		return x.xxx_hidden_ErrorMessage
	}
	return ""
}

func (x *ExportLogsPartialSuccess) SetRejectedLogRecords(v int64) {
	x.xxx_hidden_RejectedLogRecords = v
}

func (x *ExportLogsPartialSuccess) SetErrorMessage(v string) {
	x.xxx_hidden_ErrorMessage = v
}

type ExportLogsPartialSuccess_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	// The number of rejected log records.
	//
	// A `rejected_<signal>` field holding a `0` value indicates that the
	// request was fully accepted.
	RejectedLogRecords int64
	// A developer-facing human-readable message in English. It should be used
	// either to explain why the server rejected parts of the data during a partial
	// success or to convey warnings/suggestions during a full success. The message
	// should offer guidance on how users can address such issues.
	//
	// error_message is an optional field. An error_message with an empty value
	// is equivalent to it not being set.
	ErrorMessage string
}

func (b0 ExportLogsPartialSuccess_builder) Build() *ExportLogsPartialSuccess {
	m0 := &ExportLogsPartialSuccess{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_RejectedLogRecords = b.RejectedLogRecords
	x.xxx_hidden_ErrorMessage = b.ErrorMessage
	return m0
}

var File_opentelemetry_proto_collector_logs_v1_logs_service_proto protoreflect.FileDescriptor

const file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDesc = "" +
	"\n" +
	"8opentelemetry/proto/collector/logs/v1/logs_service.proto\x12%opentelemetry.proto.collector.logs.v1\x1a&opentelemetry/proto/logs/v1/logs.proto\"j\n" +
	"\x18ExportLogsServiceRequest\x12N\n" +
	"\rresource_logs\x18\x01 \x03(\v2).opentelemetry.proto.logs.v1.ResourceLogsR\fresourceLogs\"\x85\x01\n" +
	"\x19ExportLogsServiceResponse\x12h\n" +
	"\x0fpartial_success\x18\x01 \x01(\v2?.opentelemetry.proto.collector.logs.v1.ExportLogsPartialSuccessR\x0epartialSuccess\"q\n" +
	"\x18ExportLogsPartialSuccess\x120\n" +
	"\x14rejected_log_records\x18\x01 \x01(\x03R\x12rejectedLogRecords\x12#\n" +
	"\rerror_message\x18\x02 \x01(\tR\ferrorMessage2\x9d\x01\n" +
	"\vLogsService\x12\x8d\x01\n" +
	"\x06Export\x12?.opentelemetry.proto.collector.logs.v1.ExportLogsServiceRequest\x1a@.opentelemetry.proto.collector.logs.v1.ExportLogsServiceResponse\"\x00B\xd4\x01\n" +
	"(io.opentelemetry.proto.collector.logs.v1B\x10LogsServiceProtoP\x01Zlbuf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1;logsv1\xaa\x02%OpenTelemetry.Proto.Collector.Logs.V1b\x06proto3"

var file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_opentelemetry_proto_collector_logs_v1_logs_service_proto_goTypes = []any{
	(*ExportLogsServiceRequest)(nil),  // 0: opentelemetry.proto.collector.logs.v1.ExportLogsServiceRequest
	(*ExportLogsServiceResponse)(nil), // 1: opentelemetry.proto.collector.logs.v1.ExportLogsServiceResponse
	(*ExportLogsPartialSuccess)(nil),  // 2: opentelemetry.proto.collector.logs.v1.ExportLogsPartialSuccess
	(*v1.ResourceLogs)(nil),           // 3: opentelemetry.proto.logs.v1.ResourceLogs
}
var file_opentelemetry_proto_collector_logs_v1_logs_service_proto_depIdxs = []int32{
	3, // 0: opentelemetry.proto.collector.logs.v1.ExportLogsServiceRequest.resource_logs:type_name -> opentelemetry.proto.logs.v1.ResourceLogs
	2, // 1: opentelemetry.proto.collector.logs.v1.ExportLogsServiceResponse.partial_success:type_name -> opentelemetry.proto.collector.logs.v1.ExportLogsPartialSuccess
	0, // 2: opentelemetry.proto.collector.logs.v1.LogsService.Export:input_type -> opentelemetry.proto.collector.logs.v1.ExportLogsServiceRequest
	1, // 3: opentelemetry.proto.collector.logs.v1.LogsService.Export:output_type -> opentelemetry.proto.collector.logs.v1.ExportLogsServiceResponse
	3, // [3:4] is the sub-list for method output_type
	2, // [2:3] is the sub-list for method input_type
	2, // [2:2] is the sub-list for extension type_name
	2, // [2:2] is the sub-list for extension extendee
	0, // [0:2] is the sub-list for field type_name
}

func init() { file_opentelemetry_proto_collector_logs_v1_logs_service_proto_init() }
func file_opentelemetry_proto_collector_logs_v1_logs_service_proto_init() {
	if File_opentelemetry_proto_collector_logs_v1_logs_service_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDesc), len(file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_opentelemetry_proto_collector_logs_v1_logs_service_proto_goTypes,
		DependencyIndexes: file_opentelemetry_proto_collector_logs_v1_logs_service_proto_depIdxs,
		MessageInfos:      file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes,
	}.Build()
	File_opentelemetry_proto_collector_logs_v1_logs_service_proto = out.File
	file_opentelemetry_proto_collector_logs_v1_logs_service_proto_goTypes = nil
	file_opentelemetry_proto_collector_logs_v1_logs_service_proto_depIdxs = nil
}
