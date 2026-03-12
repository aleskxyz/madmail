# مهاجرت از SQLite به PostgreSQL

این راهنما نحوه تغییر دیتابیس Madmail از SQLite به PostgreSQL را شرح می‌دهد.
حالت پیش‌فرض Madmail از SQLite استفاده می‌کند که برای استقرارهای کوچک مناسب است.
برای سرورهای با ترافیک بالا یا استقرارهای حرفه‌ای، PostgreSQL توصیه می‌شود.

## پیش‌نیازها

- دسترسی root یا sudo به سرور
- Madmail نصب شده و در حال اجرا (با SQLite)
- Debian 12 یا Ubuntu 22.04+

## مرحله ۱: نصب PostgreSQL

```bash
sudo apt update
sudo apt install -y postgresql postgresql-client
```

بررسی وضعیت سرویس:

```bash
sudo systemctl status postgresql
```

## مرحله ۲: ساخت دیتابیس و کاربر

وارد کنسول PostgreSQL شوید و دیتابیس و کاربر جدید بسازید:

```bash
sudo -u postgres psql
```

سپس دستورات زیر را اجرا کنید:

```sql
CREATE DATABASE madmail;
CREATE USER madmail_user WITH PASSWORD 'YOUR_SECURE_PASSWORD';
GRANT ALL PRIVILEGES ON DATABASE madmail TO madmail_user;
ALTER DATABASE madmail OWNER TO madmail_user;
GRANT ALL ON SCHEMA public TO madmail_user;
```

برای خروج از کنسول، `\q` را تایپ کنید.

### نکات مهم درباره رمز عبور

- از رمز عبور قوی استفاده کنید (حداقل ۱۶ کاراکتر).
- فقط از **حروف انگلیسی و اعداد** استفاده کنید. از کاراکترهای خاص مانند `!`, `@`, `#` پرهیز کنید. این کاراکترها در رشته اتصال (DSN) و URL مشکل ایجاد می‌کنند.

## مرحله ۳: توقف سرویس Madmail

قبل از تغییر تنظیمات، سرویس را متوقف کنید:

```bash
sudo systemctl stop maddy
```

## مرحله ۴: پشتیبان‌گیری از تنظیمات فعلی

```bash
sudo cp /etc/maddy/maddy.conf /etc/maddy/maddy.conf.sqlite.bak
```

## مرحله ۵: ویرایش فایل تنظیمات

فایل `/etc/maddy/maddy.conf` را ویرایش کنید:

```bash
sudo nano /etc/maddy/maddy.conf
```

### ۵.۱ — تغییر بخش احراز هویت (`local_authdb`)

تنظیمات قبلی (SQLite):

```maddy
auth.pass_table local_authdb {
    auto_create yes
    table sql_table {
        driver sqlite3
        dsn credentials.db
        table_name passwords
    }
}
```

تنظیمات جدید (PostgreSQL):

```maddy
auth.pass_table local_authdb {
    auto_create yes
    table sql_table {
        driver postgres
        dsn "host=localhost user=madmail_user password=YOUR_SECURE_PASSWORD dbname=madmail sslmode=disable"
        table_name passwords
    }
}
```

### ۵.۲ — تغییر بخش ذخیره‌سازی IMAP (`local_mailboxes`)

تنظیمات قبلی (SQLite):

```maddy
storage.imapsql local_mailboxes {
    driver sqlite3
    dsn imapsql.db
    ...
}
```

تنظیمات جدید (PostgreSQL):

```maddy
storage.imapsql local_mailboxes {
    driver postgres
    dsn "host=localhost user=madmail_user password=YOUR_SECURE_PASSWORD dbname=madmail sslmode=disable"
    ...
}
```

### نکته مهم: `host=localhost`

حتماً `host=localhost` را در ابتدای رشته DSN قرار دهید. بدون آن، PostgreSQL از اتصال Unix Socket استفاده می‌کند که با احراز هویت `peer` کار می‌کند و رمز عبور را قبول نمی‌کند.

## مرحله ۶: مهاجرت داده‌ها (اختیاری)

اگر می‌خواهید داده‌های موجود (کاربران، صندوق‌های پستی، و ...) را از SQLite به PostgreSQL منتقل کنید:

### ۶.۱ — نصب ابزارها

```bash
sudo apt install -y pgloader sqlite3
```

### ۶.۲ — ادغام WAL به فایل اصلی

```bash
sudo sqlite3 /var/lib/maddy/imapsql.db 'PRAGMA wal_checkpoint(TRUNCATE);'
sudo sqlite3 /var/lib/maddy/credentials.db 'PRAGMA wal_checkpoint(TRUNCATE);'
```

### ۶.۳ — مهاجرت دیتابیس اعتبارنامه‌ها

```bash
sudo chmod 644 /var/lib/maddy/credentials.db
sudo pgloader sqlite:///var/lib/maddy/credentials.db \
    postgresql://madmail_user:YOUR_SECURE_PASSWORD@localhost/madmail
```

### ۶.۴ — مهاجرت دیتابیس IMAP

دیتابیس IMAP (`imapsql.db`) از نوع داده `LONGTEXT` استفاده می‌کند که در PostgreSQL وجود ندارد. برای حل این مشکل، یک فایل تنظیمات pgloader بسازید:

```bash
cat > /tmp/pgloader_imapsql.load << 'EOF'
LOAD DATABASE
    FROM sqlite:///var/lib/maddy/imapsql.db
    INTO postgresql://madmail_user:YOUR_SECURE_PASSWORD@localhost/madmail

CAST
    type longtext to text

WITH
    include no drop,
    create tables,
    create indexes,
    reset sequences

SET work_mem to '128MB', maintenance_work_mem to '256MB';
EOF
```

سپس اجرا کنید:

```bash
sudo chmod 644 /var/lib/maddy/imapsql.db
sudo pgloader /tmp/pgloader_imapsql.load
```

### ۶.۵ — اصلاح محدودیت‌های جداول

ابزار pgloader جداول را بدون کلید اصلی (Primary Key) ایجاد می‌کند. باید آن‌ها را به صورت دستی اضافه کنید:

```bash
sudo -u postgres psql -d madmail << 'EOF'
ALTER TABLE users ADD PRIMARY KEY (id);
ALTER TABLE users ADD UNIQUE (username);
ALTER TABLE mboxes ADD PRIMARY KEY (id);
ALTER TABLE mboxes ADD UNIQUE (uid, name);
ALTER TABLE extkeys ADD PRIMARY KEY (id);
EOF
```

### ۶.۶ — حذف و بازسازی جدول `msgs`

pgloader نمی‌تواند جدول `msgs` را به درستی منتقل کند (به دلیل تفاوت نوع داده‌ها). آن را حذف کنید تا Madmail خودش آن را بسازد:

```bash
sudo -u postgres psql -d madmail -c "DROP TABLE IF EXISTS msgs CASCADE;"
```

با راه‌اندازی مجدد Madmail، این جدول به صورت خودکار با ساختار صحیح ساخته می‌شود.

### هشدار

با حذف جدول `msgs`، فهرست پیام‌های موجود (metadata) از بین می‌رود. محتوای پیام‌ها همچنان در پوشه `/var/lib/maddy/` روی دیسک باقی می‌مانند، اما از طریق IMAP قابل دسترسی نخواهند بود. کاربران پیام‌های جدید را بدون مشکل دریافت می‌کنند.

## مرحله ۷: راه‌اندازی مجدد

```bash
sudo systemctl start maddy
```

بررسی وضعیت:

```bash
systemctl is-active maddy
```

اگر سرویس فعال نشد، لاگ‌ها را بررسی کنید:

```bash
sudo journalctl -u maddy --no-pager -n 20
```

## عیب‌یابی

### خطای `Peer authentication failed`

علت: عدم وجود `host=localhost` در رشته DSN. بدون این پارامتر، PostgreSQL از Unix Socket و احراز هویت peer استفاده می‌کند.

راه حل: `host=localhost` را به ابتدای DSN اضافه کنید.

### خطای `LONGTEXT does not exist`

علت: نوع `LONGTEXT` مخصوص SQLite/MySQL است و در PostgreSQL وجود ندارد.

راه حل: از فایل تنظیمات pgloader با دستور `CAST type longtext to text` استفاده کنید (مرحله ۶.۴).

### خطای `no unique constraint matching given keys`

علت: pgloader جداول را بدون کلید اصلی ایجاد کرده.

راه حل: کلیدهای اصلی را دستی اضافه کنید (مرحله ۶.۵).

### خطای `column must appear in GROUP BY clause`

علت: PostgreSQL سخت‌گیرتر از SQLite در مورد ستون‌های `GROUP BY` است.

راه حل: نسخه Madmail باید 0.8.104 یا بالاتر باشد. در صورت مشاهده این خطا، باینری را به‌روزرسانی کنید.

## ساختار نهایی

پس از مهاجرت موفق، PostgreSQL شامل جداول زیر خواهد بود:

| جدول | شرح |
|-----|-----|
| `passwords` | رمزهای عبور کاربران |
| `users` | اطلاعات حساب‌های IMAP |
| `mboxes` | صندوق‌های پستی |
| `msgs` | فهرست پیام‌ها |
| `flags` | پرچم‌های پیام |
| `extkeys` | کلیدهای ذخیره‌سازی خارجی |
| `quota` | سهمیه فضای ذخیره‌سازی |
| `contacts` | لینک‌های اشتراک‌گذاری مخاطبین |

## بازگشت به SQLite

در صورت نیاز به بازگشت، فایل پشتیبان را بازیابی کنید:

```bash
sudo systemctl stop maddy
sudo cp /etc/maddy/maddy.conf.sqlite.bak /etc/maddy/maddy.conf
sudo systemctl start maddy
```
