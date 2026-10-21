// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "ExampleApp",
    products: [.library(name: "ExampleApp", targets: ["App"])],
    dependencies: [
        .package(
            name: "RuntimeAlias",
            url: "https://GitHub.com/Example/Runtime.Kit.git",
            from: "1.0.0"
        ),
        .package(url: "https://example.com/acme/Test.Kit.git", branch: "main"),
        .package(url: "https://example.com/acme/Generator.git", exact: "1.0.0"),
        .package(url: "https://example.com/acme/Plugin.Kit.git", from: "1.0.0"),
        .package(id: "vendor.Logging", from: "1.0.0"),
        .package(url: "https://example.com/acme/Shared.Kit.git", from: "1.0.0"),
        .package(url: "https://example.com/acme/Unused.Kit.git", from: "1.0.0"),
        .package(path: "../LocalKit")
    ],
    targets: [
        .target(
            name: "App",
            dependencies: [
                .product(name: "Runtime", package: "RuntimeAlias"),
                .product(name: "Shared", package: "shared.kit"),
                .product(name: "Local", package: "LocalKit")
            ],
            plugins: [
                .plugin(name: "Lint", package: "plugin.kit")
            ]
        ),
        .testTarget(
            name: "AppTests",
            dependencies: [
                .target(name: "App"),
                // .product(name: "Generator", package: "generator"),
                .product(name: "Assertions", package: "test.kit"),
                .product(name: "RuntimeTesting", package: "runtime.kit"),
                .product(name: "Logging", package: "vendor.logging"),
                .product(name: "Shared", package: "shared.kit")
            ]
        ),
        .plugin(
            name: "Generate",
            capability: .buildTool(),
            dependencies: [
                .product(name: "GenerateSource", package: "generator")
            ]
        )
    ]
)
