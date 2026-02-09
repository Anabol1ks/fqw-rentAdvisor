"use client";

import { useState, useEffect } from "react";
import { adminApi } from "@/lib/admin-api";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";

interface CookieEditorProps {
  adminKey: string;
}

export function CookieEditor({ adminKey }: CookieEditorProps) {
  const [cookie, setCookie] = useState("");
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState("");

  useEffect(() => {
    setLoading(true);
    adminApi.getCookies(adminKey)
      .then((data) => setCookie(data.cookie))
      .catch((err) => setMessage("Ошибка загрузки: " + err.message))
      .finally(() => setLoading(false));
  }, [adminKey]);

  const handleSave = async () => {
    setSaving(true);
    setMessage("");
    try {
      await adminApi.updateCookies(adminKey, cookie);
      setMessage("Куки успешно сохранены!");
      setTimeout(() => setMessage(""), 3000);
    } catch (err: any) {
      setMessage("Ошибка: " + err.message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-lg">Куки (Cookie)</CardTitle>
        <CardDescription>
          Куки из файла <code className="text-xs">.env.resilient</code> для авторизации на realty.yandex.ru.
          Вставьте новые куки в Netscape-формате.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {loading ? (
          <p className="text-sm text-muted-foreground">Загрузка...</p>
        ) : (
          <>
            <Textarea
              value={cookie}
              onChange={(e) => setCookie(e.target.value)}
              className="font-mono text-xs h-[300px]"
              placeholder="# Netscape HTTP Cookie File&#10;.yandex.ru&#9;TRUE&#9;/&#9;..."
            />
            <div className="flex items-center justify-between">
              <Button onClick={handleSave} disabled={saving}>
                {saving ? "Сохранение..." : "Сохранить куки"}
              </Button>
              {message && (
                <p className={`text-sm ${message.startsWith("Ошибка") ? "text-red-500" : "text-green-600"}`}>
                  {message}
                </p>
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              Подсказка: экспортируйте куки из браузера с помощью расширения «Get cookies.txt»
              и вставьте содержимое файла выше.
            </p>
          </>
        )}
      </CardContent>
    </Card>
  );
}
