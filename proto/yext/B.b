load("@rules_proto//proto:defs.bzl", "proto_library")
load("@io_bazel_rules_go//go:def.bzl", "go_library")
load("@io_bazel_rules_go//proto:def.bzl", "go_proto_library")

go_library(
    name = "go_default_library",
    srcs = ["gen.go"],
    embed = [":yext_go_proto"],
    importpath = "github.com/michaelhenkel/ygot/proto/yext",
    visibility = ["//visibility:public"],
)

proto_library(
    name = "yext_proto",
    srcs = ["yext.proto"],
    import_prefix = "github.com/michaelhenkel/ygot/proto/yext",
    strip_import_prefix = "/proto/yext",
    visibility = ["//visibility:public"],
    deps = [
        #"@com_github_golang_protobuf//protoc-gen-go/descriptor:go_default_library",
        "@com_google_protobuf//:descriptor_proto",
    ],
)

go_proto_library(
    name = "yext_go_proto",
    importpath = "github.com/michaelhenkel/ygot/proto/yext",
    proto = ":yext_proto",
    visibility = ["//visibility:public"],
)
