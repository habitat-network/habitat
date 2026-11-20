# Moon for monorepo management

## Introduction

Moon helps manage a repository with multiple "projects" (app/service/tool/library), each with their own language, dependencies, and "tasks" (build/run/test).

Projects are listed in [.moon/workspace.yml](./workspace.yml) and point to their directories.

Tasks in [.moon/tasks.yml](./tasks.yml) and [.moon/tasks/*](./tasks/) are inherited by projects by default.

Each project can define a `moon.yml` file to override and add their own tasks.

## Official docs

https://moonrepo.dev/docs

## Explore

```
moon projects # list projects
moon tasks <project> # list all tasks for a project
moon run <project>:<task> # run a specific task
```

## Cheatsheet

### Run a task on a specific project

```
moon run [project]:[task]
```

### Run multiple tasks in parallel
```
moon [project1]:[task1] [project2]:[task2]
```

### Get info about a project like available tasks

```
moon project [project-name]
```

### Get info about a task like inputs, output, dependencies, and the actual command after inheritance resolution

```
moon task [project-name]:[task-name]
moon t [project-name]:[task-name]
```
