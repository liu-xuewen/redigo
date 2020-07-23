#!/usr/bin/env bash

setConfig() {
  git config --global user.name "github's Name"

git config --global user.email "github@xx.com"

git config --list

}

git status

git add .

git commit -m "auto commit"

git push
