# Moon for monorepo management

## Introduction

Moon help manage a repository with multiple "projects", each with their own language, dependencies, and "tasks".

Projects are listed in [.moon/workspace.yml](./workspace.yml) and point to their directories.

Tasks in [.moon/tasks.yml](./tasks.yml) and [.moon/tasks/*](./tasks/) are inherited by projects by default.

Each project can define a `moon.yml` file to override and add their own tasks.

## Official docs

https://moonrepo.dev/docs

## Installation

```
brew install moon
```

## Common tasks

These are tasks that are mostly common to all projects. 
You can run them on all projects at once or specifically on a project.

```
moon :build :test :lint :format :run
```

## Cheatsheet

### Run a task across all projects

```
moon :[task]
moon run :[task]
moon r :[task]
```

### Run a task on a specific project

```
moon [project]:[task]
moon run [project]:[task]
moon r [project]:[task]
```

### Run multiple tasks in parallel
```
moon [project]:[task1] [project]:[task2]
```

### Get info about a project like available tasks

```
moon project [project-name]
moon p [project-name]
```

### Get info about a task like inputs, output, dependencies, and the actual command

```
moon task [project-name]:[task-name]
moon t [project-name]:[task-name]
```
