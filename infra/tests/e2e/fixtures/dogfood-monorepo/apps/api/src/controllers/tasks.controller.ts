import { Controller, Get, Post, Body, Param, Delete, HttpCode, NotFoundException } from '@nestjs/common';
import { TasksService, CreateTaskDto } from '../services/tasks.service';

@Controller('tasks')
export class TasksController {
  constructor(private readonly tasksService: TasksService) {}

  @Post()
  create(@Body() dto: CreateTaskDto) {
    return this.tasksService.create(dto);
  }

  @Get()
  findAll() {
    return this.tasksService.findAll();
  }

  @Get(':id')
  findOne(@Param('id') id: string) {
    const task = this.tasksService.findById(id);
    if (!task) {
      throw new NotFoundException(`Task ${id} not found`);
    }
    return task;
  }

  @Post(':id/complete')
  @HttpCode(200)
  complete(@Param('id') id: string) {
    const task = this.tasksService.complete(id);
    if (!task) {
      throw new NotFoundException(`Task ${id} not found`);
    }
    return task;
  }

  @Delete(':id')
  @HttpCode(204)
  remove(@Param('id') id: string) {
    const removed = this.tasksService.remove(id);
    if (!removed) {
      throw new NotFoundException(`Task ${id} not found`);
    }
  }
}
