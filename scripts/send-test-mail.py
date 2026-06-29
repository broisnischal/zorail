#!/usr/bin/env python3
"""Send a sample multipart email to a local Zorail server for testing.

Usage:
    python3 scripts/send-test-mail.py [recipient] [--host H] [--port P]

Defaults: recipient=qa-1@localhost, host=127.0.0.1, port=1025
"""
import argparse
import smtplib
from email.mime.multipart import MIMEMultipart
from email.mime.text import MIMEText

p = argparse.ArgumentParser()
p.add_argument("recipient", nargs="?", default="qa-1@localhost")
p.add_argument("--host", default="127.0.0.1")
p.add_argument("--port", type=int, default=1025)
p.add_argument("--from", dest="sender", default="noreply@myapp.test")
args = p.parse_args()

code = "884217"
link = "https://app.test/verify?token=abc123"

msg = MIMEMultipart("alternative")
msg["Subject"] = "Verify your account"
msg["From"] = args.sender
msg["To"] = args.recipient
msg.attach(MIMEText(f"Your verification code is {code}.\nConfirm here: {link}\n", "plain"))
msg.attach(MIMEText(
    f"<h2>Verify your account</h2><p>Your code is <b>{code}</b>.</p>"
    f"<p><a href='{link}'>Confirm your email</a></p>", "html"))

with smtplib.SMTP(args.host, args.port) as s:
    s.send_message(msg)

print(f"✓ sent to {args.recipient} via {args.host}:{args.port}")
print(f"  open the UI and check inbox: {args.recipient}")
