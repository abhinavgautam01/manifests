// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "App",
    dependencies: [
        .package(url: "https://example.com/acme/Runtime.Kit.git", from: "1.0.0")
    ],
    targets: [
        .target(name: "App", dependencies: [
            .product(name: "Runtime", package: "runtime.kit")
        ])
    ]
)
