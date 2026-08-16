import uuid


def send_email(to: str, subject: str, body: str) -> dict:
    message_id = str(uuid.uuid4())
    print(f"[send_email] to={to} subject={subject!r} message_id={message_id}")
    print(f"[send_email] body={body!r}")
    return {"message_id": message_id, "to": to}
