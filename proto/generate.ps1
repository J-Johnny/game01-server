param(
    [string]$Protoc = "C:\Users\Administrator\AppData\Local\Python\pythoncore-3.14-64\Lib\site-packages\torch\bin\protoc.exe"
)

$ErrorActionPreference = "Stop"

$protoRoot = Join-Path $PSScriptRoot "src"
$serverRoot = Split-Path $PSScriptRoot -Parent
$repositoryRoot = Split-Path $serverRoot -Parent
$goOutput = Join-Path $PSScriptRoot "gen"
$csharpOutput = Join-Path $repositoryRoot "client\Assets\Scripts\Protocol\Generated"

New-Item -ItemType Directory -Force -Path $goOutput, $csharpOutput | Out-Null

$clientProtos = @(Get-ChildItem -Path (Join-Path $protoRoot "client") -Filter "*.proto" -Recurse |
    ForEach-Object { $_.FullName.Substring($protoRoot.Length + 1).Replace("\", "/") }
)
$internalProtos = @(Get-ChildItem -Path (Join-Path $protoRoot "internal") -Filter "*.proto" -Recurse |
    ForEach-Object { $_.FullName.Substring($protoRoot.Length + 1).Replace("\", "/") }
)

& $Protoc "--proto_path=$protoRoot" "--go_out=paths=source_relative:$goOutput" "--csharp_out=$csharpOutput" $clientProtos
if ($LASTEXITCODE -ne 0) { throw "client protocol generation failed" }

& $Protoc "--proto_path=$protoRoot" "--go_out=module=server:$serverRoot" "--go-grpc_out=module=server:$serverRoot" $internalProtos
if ($LASTEXITCODE -ne 0) { throw "internal protocol generation failed" }
