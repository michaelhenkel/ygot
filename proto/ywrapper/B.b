load("@rules_proto//proto:defs.bzl", "proto_library")
load("@io_bazel_rules_go//go:def.bzl", "go_library")
load("@io_bazel_rules_go//proto:def.bzl", "go_proto_library")

go_library(
    name = "go_default_library",
    srcs = ["gen.go"],
    embed = [":ywrapper_go_proto"],
    importpath = "github.com/michaelhenkel/ygot/proto/ywrapper",
    visibility = ["//visibility:public"],
)

proto_library(
    name = "ywrapper_proto",
    srcs = ["ywrapper.proto"],
    import_prefix = "github.com/michaelhenkel/ygot/proto/ywrapper",
    strip_import_prefix = "/proto/ywrapper",
    visibility = ["//visibility:public"],
)

go_proto_library(
    name = "ywrapper_go_proto",
    importpath = "github.com/michaelhenkel/ygot/proto/ywrapper",
    proto = ":ywrapper_proto",
    visibility = ["//visibility:public"],
)
