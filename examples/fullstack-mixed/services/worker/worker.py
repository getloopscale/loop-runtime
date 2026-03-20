import os
import time

from celery import Celery

redis_url = os.environ.get("REDIS_URL", "redis://localhost:6379/0")

app = Celery("worker", broker=redis_url, backend=redis_url)

app.conf.update(
    task_serializer="json",
    accept_content=["json"],
    result_serializer="json",
    timezone="UTC",
    enable_utc=True,
)


@app.task(bind=True, max_retries=3)
def process_task(self, task_id):
    """Process a task by ID. Simulates work with a delay."""
    try:
        time.sleep(2)
        return {"task_id": task_id, "status": "completed"}
    except Exception as exc:
        raise self.retry(exc=exc, countdown=5)


@app.task
def send_notification(user_id, message):
    """Send a notification to a user."""
    return {"user_id": user_id, "message": message, "sent": True}


@app.task
def cleanup_old_tasks(days=30):
    """Remove tasks older than the specified number of days."""
    return {"cleaned": True, "days": days}
