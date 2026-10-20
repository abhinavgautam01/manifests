// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "Runtime.Kit",
    products: [.library(name: "Runtime", targets: ["Runtime"])],
    dependencies: [
        .package(url: "https://example.com/acme/Support.Kit.git", from: "1.0.0")
    ],
    targets: [
        .target(name: "Runtime", dependencies: [
            .product(name: "Support", package: "support.kit")
        ])
    ]
)
