#!/bin/sh
# Generate Python gRPC stubs (scorer_pb2.py + scorer_pb2_grpc.py) from scorer.proto
# into the current dir, so server.py can `import scorer_pb2`.
set -e
python -m grpc_tools.protoc -I proto/scorer/v1 \
  --python_out=. --grpc_python_out=. scorer.proto
