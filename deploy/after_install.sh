#!/bin/bash

USER="cortex-tenant"
HOME="/var/lib/$USER"

useradd -d $HOME -s /bin/false -M $USER > /dev/null 2>&1 || true
chown $USER:$USER $HOME
