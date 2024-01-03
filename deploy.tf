terraform {
  backend "s3" {
    bucket = "sod-auctions-deployments"
    key    = "terraform/auctions_uploader_job"
    region = "us-east-1"
  }
}

provider "aws" {
  region = "us-east-1"
}

variable "app_name" {
  type    = string
  default = "auctions_uploader_job"
}

data "archive_file" "lambda_zip" {
  type        = "zip"
  source_file = "${path.module}/bootstrap"
  output_path = "${path.module}/lambda_function.zip"
}

data "local_file" "lambda_zip_contents" {
  filename = data.archive_file.lambda_zip.output_path
}

data "aws_ssm_parameter" "db_connection_string" {
  name = "/db-connection-string"
}

data "aws_ssm_parameter" "blizzard_client_id" {
  name = "/blizzard-client-id"
}

data "aws_ssm_parameter" "blizzard_client_secret" {
  name = "/blizzard-client-secret"
}

resource "aws_iam_role" "lambda_exec" {
  name               = "${var.app_name}_job_execution_role"
  assume_role_policy = jsonencode({
    "Version" : "2012-10-17",
    "Statement" : [
      {
        "Action" : "sts:AssumeRole",
        "Principal" : {
          "Service" : "lambda.amazonaws.com"
        },
        "Effect" : "Allow"
      }
    ]
  })
}

resource "aws_iam_role_policy" "s3_access_policy" {
  role   = aws_iam_role.lambda_exec.id
  policy = jsonencode({
    "Version" : "2012-10-17",
    "Statement" : [
      {
        "Effect" : "Allow",
        "Action" : [
          "s3:PutObject"
        ],
        "Resource" : [
          "arn:aws:s3:::sod-auctions/*"
        ]
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_vpc_execution_role" {
  role       = aws_iam_role.lambda_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

resource "aws_lambda_function" "auctions_uploader_job" {
  function_name    = var.app_name
  architectures    = ["arm64"]
  memory_size      = 512
  handler          = "bootstrap"
  role             = aws_iam_role.lambda_exec.arn
  filename         = data.archive_file.lambda_zip.output_path
  source_code_hash = data.local_file.lambda_zip_contents.content_md5
  runtime          = "provided.al2023"
  timeout          = 120

  environment {
    variables = {
      DB_CONNECTION_STRING = data.aws_ssm_parameter.db_connection_string.value
      BLIZZARD_CLIENT_ID = data.aws_ssm_parameter.blizzard_client_id.value
      BLIZZARD_CLIENT_SECRET = data.aws_ssm_parameter.blizzard_client_secret.value
    }
  }
}

resource "aws_cloudwatch_event_rule" "scheduler" {
  name                = "scheduler_${aws_lambda_function.auctions_uploader_job.function_name}"
  schedule_expression = "cron(0 * * * ? *)"
}

resource "aws_cloudwatch_event_target" "run_target" {
  rule      = aws_cloudwatch_event_rule.scheduler.name
  target_id = "lambda_function"
  arn       = aws_lambda_function.auctions_uploader_job.arn
}

resource "aws_lambda_permission" "allow_cloudwatch" {
  statement_id  = "AllowExecutionFromCloudWatch"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.auctions_uploader_job.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.scheduler.arn
}