import { Injectable } from '@nestjs/common';
import { generateId } from '@dogfood/shared-utils';

export interface CreateTaskDto {
  title: string;
  description?: string;
  priority?: 'low' | 'medium' | 'high';
}

export interface Task {
  id: string;
  title: string;
  description: string;
  priority: 'low' | 'medium' | 'high';
  completed: boolean;
  createdAt: string;
  completedAt: string | null;
}

@Injectable()
export class TasksService {
  private tasks: Map<string, Task> = new Map();

  create(dto: CreateTaskDto): Task {
    const task: Task = {
      id: generateId('task'),
      title: dto.title,
      description: dto.description || '',
      priority: dto.priority || 'medium',
      completed: false,
      createdAt: new Date().toISOString(),
      completedAt: null,
    };
    this.tasks.set(task.id, task);
    return task;
  }

  findAll(): Task[] {
    return Array.from(this.tasks.values());
  }

  findById(id: string): Task | undefined {
    return this.tasks.get(id);
  }

  complete(id: string): Task | undefined {
    const task = this.tasks.get(id);
    if (!task) return undefined;
    task.completed = true;
    task.completedAt = new Date().toISOString();
    return task;
  }

  remove(id: string): boolean {
    return this.tasks.delete(id);
  }
}
