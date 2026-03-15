import { Module } from '@nestjs/common';
import { HealthController } from './controllers/health.controller';
import { TasksController } from './controllers/tasks.controller';
import { TasksService } from './services/tasks.service';

@Module({
  controllers: [HealthController, TasksController],
  providers: [TasksService],
})
export class AppModule {}
