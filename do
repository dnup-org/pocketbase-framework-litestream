#!/usr/bin/env bash
# ./do is a wrapper that sources a known environment file (.auth.env) and then
# executes the arguments in bash.
#
# Usage Examples:
# ./do python my_script.py       # injects environment
# ./do 'echo $AWS_SECRET_TOKEN'  # single quotes delays evaluation of variables
#                                #   until they are injected by ./do
export PATH=$PWD:$PATH
set -a # force export
. .auth.env
set +a # turn off force export
eval "$@"
