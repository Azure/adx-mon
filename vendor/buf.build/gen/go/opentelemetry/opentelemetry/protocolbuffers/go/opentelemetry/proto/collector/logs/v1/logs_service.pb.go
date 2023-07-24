// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.31.0
// 	protoc        (unknown)
// source: opentelemetry/proto/collector/logs/v1/logs_service.proto

package logsv1

import (
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type ExportLogsServiceRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	ResourceLogs []*v1.ResourceLogs `protobuf:"bytes,1,rep,name=resource_logs,json=resourceLogs,proto3" json:"resource_logs,omitempty"`
}

func (x *ExportLogsServiceRequest) Reset() {
	*x = ExportLogsServiceRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *ExportLogsServiceRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ExportLogsServiceRequest) ProtoMessage() {}

func (x *ExportLogsServiceRequest) ProtoReflect() protoreflect.Message {
	mi := &file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ExportLogsServiceRequest.ProtoReflect.Descriptor instead.
func (*ExportLogsServiceRequest) Descriptor() ([]byte, []int) {
	return file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDescGZIP(), []int{0}
}

func (x *ExportLogsServiceRequest) GetResourceLogs() []*v1.ResourceLogs {
	if x != nil {
		return x.ResourceLogs
	}
	return nil
}

type ExportLogsServiceResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	PartialSuccess *ExportLogsPartialSuccess `protobuf:"bytes,1,opt,name=partial_success,json=partialSuccess,proto3" json:"partial_success,omitempty"`
}

func (x *ExportLogsServiceResponse) Reset() {
	*x = ExportLogsServiceResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *ExportLogsServiceResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ExportLogsServiceResponse) ProtoMessage() {}

func (x *ExportLogsServiceResponse) ProtoReflect() protoreflect.Message {
	mi := &file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ExportLogsServiceResponse.ProtoReflect.Descriptor instead.
func (*ExportLogsServiceResponse) Descriptor() ([]byte, []int) {
	return file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDescGZIP(), []int{1}
}

func (x *ExportLogsServiceResponse) GetPartialSuccess() *ExportLogsPartialSuccess {
	if x != nil {
		return x.PartialSuccess
	}
	return nil
}

type ExportLogsPartialSuccess struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	RejectedLogRecords int64  `protobuf:"varint,1,opt,name=rejected_log_records,json=rejectedLogRecords,proto3" json:"rejected_log_records,omitempty"`
	ErrorMessage       string `protobuf:"bytes,2,opt,name=error_message,json=errorMessage,proto3" json:"error_message,omitempty"`
}

func (x *ExportLogsPartialSuccess) Reset() {
	*x = ExportLogsPartialSuccess{}
	if protoimpl.UnsafeEnabled {
		mi := &file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *ExportLogsPartialSuccess) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ExportLogsPartialSuccess) ProtoMessage() {}

func (x *ExportLogsPartialSuccess) ProtoReflect() protoreflect.Message {
	mi := &file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ExportLogsPartialSuccess.ProtoReflect.Descriptor instead.
func (*ExportLogsPartialSuccess) Descriptor() ([]byte, []int) {
	return file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDescGZIP(), []int{2}
}

func (x *ExportLogsPartialSuccess) GetRejectedLogRecords() int64 {
	if x != nil {
		return x.RejectedLogRecords
	}
	return 0
}

func (x *ExportLogsPartialSuccess) GetErrorMessage() string {
	if x != nil {
		return x.ErrorMessage
	}
	return ""
}

var File_opentelemetry_proto_collector_logs_v1_logs_service_proto protoreflect.FileDescriptor

var file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDesc = []byte{
	0x0a, 0x38, 0x6f, 0x70, 0x65, 0x6e, 0x74, 0x65, 0x6c, 0x65, 0x6d, 0x65, 0x74, 0x72, 0x79, 0x2f,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x63, 0x6f, 0x6c, 0x6c, 0x65, 0x63, 0x74, 0x6f, 0x72, 0x2f,
	0x6c, 0x6f, 0x67, 0x73, 0x2f, 0x76, 0x31, 0x2f, 0x6c, 0x6f, 0x67, 0x73, 0x5f, 0x73, 0x65, 0x72,
	0x76, 0x69, 0x63, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x25, 0x6f, 0x70, 0x65, 0x6e,
	0x74, 0x65, 0x6c, 0x65, 0x6d, 0x65, 0x74, 0x72, 0x79, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2e,
	0x63, 0x6f, 0x6c, 0x6c, 0x65, 0x63, 0x74, 0x6f, 0x72, 0x2e, 0x6c, 0x6f, 0x67, 0x73, 0x2e, 0x76,
	0x31, 0x1a, 0x26, 0x6f, 0x70, 0x65, 0x6e, 0x74, 0x65, 0x6c, 0x65, 0x6d, 0x65, 0x74, 0x72, 0x79,
	0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x6c, 0x6f, 0x67, 0x73, 0x2f, 0x76, 0x31, 0x2f, 0x6c,
	0x6f, 0x67, 0x73, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0x6a, 0x0a, 0x18, 0x45, 0x78, 0x70,
	0x6f, 0x72, 0x74, 0x4c, 0x6f, 0x67, 0x73, 0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x52, 0x65,
	0x71, 0x75, 0x65, 0x73, 0x74, 0x12, 0x4e, 0x0a, 0x0d, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63,
	0x65, 0x5f, 0x6c, 0x6f, 0x67, 0x73, 0x18, 0x01, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x29, 0x2e, 0x6f,
	0x70, 0x65, 0x6e, 0x74, 0x65, 0x6c, 0x65, 0x6d, 0x65, 0x74, 0x72, 0x79, 0x2e, 0x70, 0x72, 0x6f,
	0x74, 0x6f, 0x2e, 0x6c, 0x6f, 0x67, 0x73, 0x2e, 0x76, 0x31, 0x2e, 0x52, 0x65, 0x73, 0x6f, 0x75,
	0x72, 0x63, 0x65, 0x4c, 0x6f, 0x67, 0x73, 0x52, 0x0c, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63,
	0x65, 0x4c, 0x6f, 0x67, 0x73, 0x22, 0x85, 0x01, 0x0a, 0x19, 0x45, 0x78, 0x70, 0x6f, 0x72, 0x74,
	0x4c, 0x6f, 0x67, 0x73, 0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x52, 0x65, 0x73, 0x70, 0x6f,
	0x6e, 0x73, 0x65, 0x12, 0x68, 0x0a, 0x0f, 0x70, 0x61, 0x72, 0x74, 0x69, 0x61, 0x6c, 0x5f, 0x73,
	0x75, 0x63, 0x63, 0x65, 0x73, 0x73, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x3f, 0x2e, 0x6f,
	0x70, 0x65, 0x6e, 0x74, 0x65, 0x6c, 0x65, 0x6d, 0x65, 0x74, 0x72, 0x79, 0x2e, 0x70, 0x72, 0x6f,
	0x74, 0x6f, 0x2e, 0x63, 0x6f, 0x6c, 0x6c, 0x65, 0x63, 0x74, 0x6f, 0x72, 0x2e, 0x6c, 0x6f, 0x67,
	0x73, 0x2e, 0x76, 0x31, 0x2e, 0x45, 0x78, 0x70, 0x6f, 0x72, 0x74, 0x4c, 0x6f, 0x67, 0x73, 0x50,
	0x61, 0x72, 0x74, 0x69, 0x61, 0x6c, 0x53, 0x75, 0x63, 0x63, 0x65, 0x73, 0x73, 0x52, 0x0e, 0x70,
	0x61, 0x72, 0x74, 0x69, 0x61, 0x6c, 0x53, 0x75, 0x63, 0x63, 0x65, 0x73, 0x73, 0x22, 0x71, 0x0a,
	0x18, 0x45, 0x78, 0x70, 0x6f, 0x72, 0x74, 0x4c, 0x6f, 0x67, 0x73, 0x50, 0x61, 0x72, 0x74, 0x69,
	0x61, 0x6c, 0x53, 0x75, 0x63, 0x63, 0x65, 0x73, 0x73, 0x12, 0x30, 0x0a, 0x14, 0x72, 0x65, 0x6a,
	0x65, 0x63, 0x74, 0x65, 0x64, 0x5f, 0x6c, 0x6f, 0x67, 0x5f, 0x72, 0x65, 0x63, 0x6f, 0x72, 0x64,
	0x73, 0x18, 0x01, 0x20, 0x01, 0x28, 0x03, 0x52, 0x12, 0x72, 0x65, 0x6a, 0x65, 0x63, 0x74, 0x65,
	0x64, 0x4c, 0x6f, 0x67, 0x52, 0x65, 0x63, 0x6f, 0x72, 0x64, 0x73, 0x12, 0x23, 0x0a, 0x0d, 0x65,
	0x72, 0x72, 0x6f, 0x72, 0x5f, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x18, 0x02, 0x20, 0x01,
	0x28, 0x09, 0x52, 0x0c, 0x65, 0x72, 0x72, 0x6f, 0x72, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65,
	0x32, 0x9d, 0x01, 0x0a, 0x0b, 0x4c, 0x6f, 0x67, 0x73, 0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65,
	0x12, 0x8d, 0x01, 0x0a, 0x06, 0x45, 0x78, 0x70, 0x6f, 0x72, 0x74, 0x12, 0x3f, 0x2e, 0x6f, 0x70,
	0x65, 0x6e, 0x74, 0x65, 0x6c, 0x65, 0x6d, 0x65, 0x74, 0x72, 0x79, 0x2e, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x2e, 0x63, 0x6f, 0x6c, 0x6c, 0x65, 0x63, 0x74, 0x6f, 0x72, 0x2e, 0x6c, 0x6f, 0x67, 0x73,
	0x2e, 0x76, 0x31, 0x2e, 0x45, 0x78, 0x70, 0x6f, 0x72, 0x74, 0x4c, 0x6f, 0x67, 0x73, 0x53, 0x65,
	0x72, 0x76, 0x69, 0x63, 0x65, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x40, 0x2e, 0x6f,
	0x70, 0x65, 0x6e, 0x74, 0x65, 0x6c, 0x65, 0x6d, 0x65, 0x74, 0x72, 0x79, 0x2e, 0x70, 0x72, 0x6f,
	0x74, 0x6f, 0x2e, 0x63, 0x6f, 0x6c, 0x6c, 0x65, 0x63, 0x74, 0x6f, 0x72, 0x2e, 0x6c, 0x6f, 0x67,
	0x73, 0x2e, 0x76, 0x31, 0x2e, 0x45, 0x78, 0x70, 0x6f, 0x72, 0x74, 0x4c, 0x6f, 0x67, 0x73, 0x53,
	0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x22, 0x00,
	0x42, 0xd4, 0x01, 0x0a, 0x28, 0x69, 0x6f, 0x2e, 0x6f, 0x70, 0x65, 0x6e, 0x74, 0x65, 0x6c, 0x65,
	0x6d, 0x65, 0x74, 0x72, 0x79, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2e, 0x63, 0x6f, 0x6c, 0x6c,
	0x65, 0x63, 0x74, 0x6f, 0x72, 0x2e, 0x6c, 0x6f, 0x67, 0x73, 0x2e, 0x76, 0x31, 0x42, 0x10, 0x4c,
	0x6f, 0x67, 0x73, 0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x50, 0x72, 0x6f, 0x74, 0x6f, 0x50,
	0x01, 0x5a, 0x6c, 0x62, 0x75, 0x66, 0x2e, 0x62, 0x75, 0x69, 0x6c, 0x64, 0x2f, 0x67, 0x65, 0x6e,
	0x2f, 0x67, 0x6f, 0x2f, 0x6f, 0x70, 0x65, 0x6e, 0x74, 0x65, 0x6c, 0x65, 0x6d, 0x65, 0x74, 0x72,
	0x79, 0x2f, 0x6f, 0x70, 0x65, 0x6e, 0x74, 0x65, 0x6c, 0x65, 0x6d, 0x65, 0x74, 0x72, 0x79, 0x2f,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x63, 0x6f, 0x6c, 0x62, 0x75, 0x66, 0x66, 0x65, 0x72, 0x73, 0x2f,
	0x67, 0x6f, 0x2f, 0x6f, 0x70, 0x65, 0x6e, 0x74, 0x65, 0x6c, 0x65, 0x6d, 0x65, 0x74, 0x72, 0x79,
	0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x63, 0x6f, 0x6c, 0x6c, 0x65, 0x63, 0x74, 0x6f, 0x72,
	0x2f, 0x6c, 0x6f, 0x67, 0x73, 0x2f, 0x76, 0x31, 0x3b, 0x6c, 0x6f, 0x67, 0x73, 0x76, 0x31, 0xaa,
	0x02, 0x25, 0x4f, 0x70, 0x65, 0x6e, 0x54, 0x65, 0x6c, 0x65, 0x6d, 0x65, 0x74, 0x72, 0x79, 0x2e,
	0x50, 0x72, 0x6f, 0x74, 0x6f, 0x2e, 0x43, 0x6f, 0x6c, 0x6c, 0x65, 0x63, 0x74, 0x6f, 0x72, 0x2e,
	0x4c, 0x6f, 0x67, 0x73, 0x2e, 0x56, 0x31, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDescOnce sync.Once
	file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDescData = file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDesc
)

func file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDescGZIP() []byte {
	file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDescOnce.Do(func() {
		file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDescData = protoimpl.X.CompressGZIP(file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDescData)
	})
	return file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDescData
}

var file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_opentelemetry_proto_collector_logs_v1_logs_service_proto_goTypes = []interface{}{
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
	if !protoimpl.UnsafeEnabled {
		file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*ExportLogsServiceRequest); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*ExportLogsServiceResponse); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_opentelemetry_proto_collector_logs_v1_logs_service_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*ExportLogsPartialSuccess); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDesc,
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
	file_opentelemetry_proto_collector_logs_v1_logs_service_proto_rawDesc = nil
	file_opentelemetry_proto_collector_logs_v1_logs_service_proto_goTypes = nil
	file_opentelemetry_proto_collector_logs_v1_logs_service_proto_depIdxs = nil
}