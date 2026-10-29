param([Parameter(Mandatory = $true)][string]$SetupExe)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()
$utf8 = New-Object System.Text.UTF8Encoding($false)
$script:busy = $false

function Invoke-Backend([string]$Action, [string]$InputJson = '{}') {
    $info = New-Object System.Diagnostics.ProcessStartInfo
    $info.FileName = $SetupExe
    $info.Arguments = $Action
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardInput = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    $info.StandardInputEncoding = $utf8
    $info.StandardOutputEncoding = $utf8
    $info.StandardErrorEncoding = $utf8
    $process = New-Object System.Diagnostics.Process
    $process.StartInfo = $info
    try {
        [void]$process.Start()
        $output = $process.StandardOutput.ReadToEndAsync()
        $errors = $process.StandardError.ReadToEndAsync()
        $process.StandardInput.Write($InputJson)
        $process.StandardInput.Close()
        while (-not $process.WaitForExit(100)) {
            [System.Windows.Forms.Application]::DoEvents()
        }
        $result = $output.GetAwaiter().GetResult() | ConvertFrom-Json
        if (-not $result.OK) { throw $result.Message }
        return $result
    } finally {
        $process.Dispose()
    }
}

function Add-Shortcut([string]$Name, [string]$Target, [string]$Arguments = '') {
    $menu = Join-Path ([Environment]::GetFolderPath('Programs')) 'SOHO Review'
    [void][System.IO.Directory]::CreateDirectory($menu)
    $shell = New-Object -ComObject WScript.Shell
    $shortcut = $shell.CreateShortcut((Join-Path $menu ($Name + '.lnk')))
    $shortcut.TargetPath = $Target
    $shortcut.Arguments = $Arguments
    $shortcut.Save()
}

$form = New-Object System.Windows.Forms.Form
$form.Text = 'SOHO — настройка проверки работ'
$form.ClientSize = New-Object System.Drawing.Size(620, 475)
$form.StartPosition = 'CenterScreen'
$form.FormBorderStyle = 'FixedDialog'
$form.MaximizeBox = $false
$form.Font = New-Object System.Drawing.Font('Segoe UI', 10)

$intro = New-Object System.Windows.Forms.Label
$intro.Text = "1. Введите логин и пароль SOHO и нажмите «Установить / обновить».`r`n2. Перезапустите ChatGPT Desktop.`r`n3. В новом диалоге выберите навык soho-homework-review."
$intro.SetBounds(20, 15, 580, 80)
$form.Controls.Add($intro)

$loginLabel = New-Object System.Windows.Forms.Label
$loginLabel.Text = 'Логин SOHO'
$loginLabel.SetBounds(20, 105, 145, 26)
$form.Controls.Add($loginLabel)
$login = New-Object System.Windows.Forms.TextBox
$login.SetBounds(170, 102, 420, 28)
$form.Controls.Add($login)

$passwordLabel = New-Object System.Windows.Forms.Label
$passwordLabel.Text = 'Пароль SOHO'
$passwordLabel.SetBounds(20, 145, 145, 26)
$form.Controls.Add($passwordLabel)
$password = New-Object System.Windows.Forms.TextBox
$password.UseSystemPasswordChar = $true
$password.SetBounds(170, 142, 420, 28)
$form.Controls.Add($password)

$hint = New-Object System.Windows.Forms.Label
$hint.Text = "При обновлении оставьте оба поля пустыми, чтобы сохранить доступ.`r`nВаш SKILL.md сохраняется при обновлении программы."
$hint.SetBounds(20, 184, 580, 50)
$form.Controls.Add($hint)

$install = New-Object System.Windows.Forms.Button
$install.Text = 'Установить / обновить'
$install.SetBounds(20, 245, 280, 40)
$form.Controls.Add($install)
$check = New-Object System.Windows.Forms.Button
$check.Text = 'Проверить подключение'
$check.SetBounds(320, 245, 280, 40)
$form.Controls.Add($check)
$edit = New-Object System.Windows.Forms.Button
$edit.Text = 'Изменить правила проверки'
$edit.SetBounds(20, 298, 280, 40)
$form.Controls.Add($edit)
$materials = New-Object System.Windows.Forms.Button
$materials.Text = 'Открыть материалы'
$materials.SetBounds(320, 298, 280, 40)
$form.Controls.Add($materials)

$status = New-Object System.Windows.Forms.Label
$status.Text = 'Готово к настройке'
$status.SetBounds(20, 355, 580, 105)
$form.Controls.Add($status)
$controls = @($install, $check, $edit, $materials, $login, $password)

function Invoke-Action([scriptblock]$Action) {
    $script:busy = $true
    foreach ($control in $controls) { $control.Enabled = $false }
    $form.UseWaitCursor = $true
    try { & $Action } catch {
        $status.Text = 'Действие не завершено'
        [void][System.Windows.Forms.MessageBox]::Show($_.Exception.Message, 'Настройка SOHO', 'OK', 'Error')
    } finally {
        $form.UseWaitCursor = $false
        foreach ($control in $controls) { $control.Enabled = $true }
        $script:busy = $false
    }
}

$install.Add_Click({ Invoke-Action {
    $status.Text = 'Установка и настройка подключения...'
    $request = @{ Login = $login.Text; Password = $password.Text } | ConvertTo-Json -Compress
    try { $result = Invoke-Backend '--install' $request } finally {
        $password.Clear()
        $request = $null
    }
    $paths = $result.Paths
    try {
        Add-Shortcut 'Настройка SOHO' (Join-Path $paths.Directory 'SOHO-Setup.exe')
        Add-Shortcut 'Изменить правила проверки' (Join-Path $env:SystemRoot 'System32\notepad.exe') ('"' + $paths.Skill + '"')
        Add-Shortcut 'Материалы SOHO' (Join-Path $env:SystemRoot 'explorer.exe') ('"' + $paths.Materials + '"')
        $status.Text = "Установлено. Перезапустите ChatGPT Desktop.`r`nЯрлыки находятся в меню «Пуск» → SOHO Review.`r`nПроверка подключения читает очередь, не изменяя работы."
    } catch {
        $status.Text = 'Установлено, но не удалось создать ярлыки. Кнопки этого окна доступны'
    }
} })

$check.Add_Click({ Invoke-Action {
    $status.Text = 'Проверка входа и инструментов MCP, подождите...'
    [void](Invoke-Backend '--check')
    $status.Text = "Вход и MCP работают. Данные SOHO не изменены.`r`nЧтение ссылок и качество проверки испытайте в ChatGPT.`r`nДля каждого ученика начинайте новый диалог."
} })

$edit.Add_Click({ Invoke-Action {
    $paths = (Invoke-Backend '--status').Paths
    if (-not (Test-Path -LiteralPath $paths.Skill -PathType Leaf)) { throw 'Сначала установите программу' }
    Start-Process -FilePath (Join-Path $env:SystemRoot 'System32\notepad.exe') -ArgumentList ('"' + $paths.Skill + '"')
    $status.Text = "Измените текст SKILL.md и сохраните файл.`r`nСохраните поля name и description в начале файла.`r`nПроверяйте новые правила в новом диалоге ChatGPT."
} })

$materials.Add_Click({ Invoke-Action {
    $paths = (Invoke-Backend '--status').Paths
    if (-not (Test-Path -LiteralPath $paths.Materials -PathType Container)) { throw 'Сначала установите программу' }
    Start-Process -FilePath (Join-Path $env:SystemRoot 'explorer.exe') -ArgumentList ('"' + $paths.Materials + '"')
} })

$form.Add_FormClosing({ param($sender, $eventArgs) if ($script:busy) { $eventArgs.Cancel = $true } })
[void]$form.ShowDialog()
