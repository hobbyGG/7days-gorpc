# readme

Codec 模块：存放传输过程编解码相关的类型与逻辑。
RPC 模块：存放框架服务的代码。
Main 模块：实现了最简单的通信过程。
详细介绍
Codec 模块
功能：负责处理数据的编码和解码，确保数据在传输过程中的正确性和完整性。
相关文件：codec.go
RPC 模块
功能：提供 RPC 服务的核心逻辑，包括服务注册、调用和处理等。
相关文件：rpc.go
Main 模块
功能：实现了一个简单的客户端和服务器端通信示例，展示了 RPC 框架的基本使用方法。
相关文件：main.go
项目结构

plaintext
.
├── codec
│   └── codec.go
├── rpc
│   └── rpc.go
└── main
    └── main.go
如何使用
运行示例：

在项目根目录下运行 go run main/main.go 启动服务器。
另起一个终端窗口，运行 go run main/main.go client 启动客户端。
自定义使用：

根据实际需求，扩展或修改 Codec 和 RPC 模块中的代码。
在 main 模块中添加或修改示例代码，以满足特定的通信需求。
